package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"io/ioutil"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	proto "github.com/denizaydin/nmon/api"
	nmonclient "github.com/denizaydin/nmon/client"
	logrus "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

// configServer: Configuration Server that will push monitoring information
var configServer *string

// statsServer: Statistic reporting server that we will send monitoring results
var statsServer *string

// willingTobeMonitored: Do we want to be monitored by other clients?
var willingTobeMonitored bool

// clientName
var clientName string

// clientGroups
var groups *string

//client: New broadcast client for configuration
var configclient proto.ConfigServerClient

//wait: Global wail group for control
var wait *sync.WaitGroup

//conn: Current GRPC connection to the server
var configconn *grpc.ClientConn

//client - Pointer of the current client
var client *nmonclient.NmonClient

var logging *logrus.Logger

func init() {
	client = &nmonclient.NmonClient{
		ConfigClient:            &proto.Client{},
		IsConfigClientConnected: false,
		StatsClient:             &proto.Client{},
		IsStatsClientConnected:  false,
		MonObecjts:              map[string]*nmonclient.MonObject{},
		MonObjectScanTimer:      &time.Ticker{},
		Logging:                 &logrus.Logger{},
		WaitChannel:             make(chan int),
		WaitGroup:               wait,
	}
	// Log as JSON instead of the default ASCII formatter.
	client.Logging = logrus.New()
	client.Logging.SetFormatter(&logrus.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})
	// Output to stdout instead of the default stderr
	// Can be any io.Writer, see below for File example
	client.Logging.SetOutput(os.Stdout)
	logLevel := "info"
	configServer = flag.String("configServer", "127.0.0.1:8080", "current environment")
	statsServer = flag.String("statsServer", "127.0.0.1:8081", "port number")
	flag.StringVar(&logLevel, "loglevel", "disable", "disable, info, error, warning,debug or trace")
	flag.BoolVar(&client.ConfigClient.AddAsPingDest, "isPingDest", false, "willing to be pinged")
	flag.BoolVar(&client.ConfigClient.AddAsTraceDest, "isTraceDest", false, "willing to be traced")
	flag.BoolVar(&client.ConfigClient.AddAsAppDest, "isAppDest", false, "willing to be monitored by app, not implemented")
	flag.StringVar(&client.ConfigClient.Name, "clientName", "", "name to be used as identifier on the server, operating system name will be used as a default")
	groups = flag.String("groups", "default", "client groups separeted by comma")
	flag.Parse()
	switch logLevel {
	case "disable":
		client.Logging.SetOutput(ioutil.Discard)
	case "info":
		client.Logging.SetLevel(logrus.InfoLevel)
	case "error":
		client.Logging.SetLevel(logrus.ErrorLevel)
	case "warn":
		client.Logging.SetLevel(logrus.WarnLevel)
	case "debug":
		client.Logging.SetLevel(logrus.DebugLevel)
	case "trace":
		client.Logging.SetLevel(logrus.TraceLevel)
	default:
		client.Logging.SetLevel(logrus.DebugLevel)
	}

	if client.ConfigClient.Name == "" {
		hostname, err := os.Hostname()
		if err != nil {
			client.Logging.Fatalf("client name is required and we can not get the hostname")
		} else {
			client.Logging.Warnf("no client name is given, setting clientname to hostname:%v", hostname)
			client.ConfigClient.Name = hostname
		}
	}
	clientGroups := make(map[string]string)
	for _, pair := range strings.Split(*groups, ",") {
		clientGroups[pair] = pair
	}
	id := sha256.Sum256([]byte(time.Now().String() + clientName))
	client.ConfigClient = &proto.Client{
		Id:             hex.EncodeToString(id[:]),
		Name:           client.ConfigClient.Name,
		Groups:         clientGroups,
		AddAsPingDest:  client.ConfigClient.AddAsPingDest,
		AddAsTraceDest: client.ConfigClient.AddAsTraceDest,
		AddAsAppDest:   client.ConfigClient.AddAsAppDest,
	}
	client.StatsClient = &proto.Client{
		Id:     hex.EncodeToString(id[:]),
		Name:   client.ConfigClient.Name,
		Groups: clientGroups,
	}
	client.WaitGroup = &sync.WaitGroup{}
	client.Logging.Info("initilazed monitoring client service")
	client.Logging.Debugf("client parameters are:%v", client)
}

//getMonitoringObjects - retrives monitoring object from a GRPC server
func getMonitoringObjects(client *nmonclient.NmonClient) {
	client.Logging.Tracef("retriving configuration from:%v", configServer)
	for {
		if !client.IsConfigClientConnected {
			client.Logging.Infof("tring to connect config server:%v with name:%v and id:%v", *configServer, client.ConfigClient.GetName(), client.ConfigClient.GetId())
			configconn, err := grpc.Dial(*configServer, grpc.WithInsecure(), grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                1 * time.Second, // send pings every 10 seconds if there is no activity
				Timeout:             time.Second,     // wait 1 second for ping ack before considering the connection dead
				PermitWithoutStream: true,            // send pings even without active streams
			}), grpc.WithDefaultServiceConfig(`{
				"methodConfig": [{
				  "name": [{"service": "nmon client service"}],
				  "waitForReady": true,
				  "retryPolicy": {
					  "MaxAttempts": 4,
					  "InitialBackoff": "1s",
					  "MaxBackoff": "6s",
					  "BackoffMultiplier": 1.5,
					  "RetryableStatusCodes": [ "UNAVAILABLE" ]
				  }
				}]}`), grpc.WithBlock())
			if err != nil {
				client.Logging.Errorf("could not connect to config service: %v, waiting for 10sec to retry", err)
				time.Sleep(10 * time.Second)
				break
			}
			client.Logging.Debug("connected to the configuration server, registering")
			configclient = proto.NewConfigServerClient(configconn)
			stream, err := configclient.CreateStream(context.Background(), &proto.Connect{
				Client: client.ConfigClient,
			})
			if err != nil {
				client.Logging.Errorf("configuration service registration failed: %v, waiting for 10sec to retry", err)
				time.Sleep(10 * time.Second)
				break
			}
			client.IsConfigClientConnected = true
			client.WaitGroup.Add(1)
			client.Logging.Info("configuration service is registred, waiting for monitoring object to be streamed")
			go func(str proto.ConfigServer_CreateStreamClient) {
				defer client.WaitGroup.Done()
				for {
					client.Logging.Tracef("reading stream messages from the config server")
					monitoringObject, err := str.Recv()
					if err != nil {
						client.Logging.Errorf("error reading configuration message: %v", err)
						client.IsConfigClientConnected = false
						break
					}
					client.Logging.Debugf("received configuration message %s", monitoringObject)
					// adding configuration objects into conf objects map. As same destinstion can be added for multiple type, uniquness is needed for the map key.
					switch t := monitoringObject.Object.(type) {
					case *proto.MonitoringObject_Pingdest:
						client.MonObecjts[monitoringObject.GetPingdest().GetDestination()+"-ping"] = &nmonclient.MonObject{
							ConfigurationUpdatetime: time.Now().UnixNano(),
							Object:                  monitoringObject,
						}
						// Configuration checks
						// Check interval
						if client.MonObecjts[monitoringObject.GetPingdest().GetDestination()+"-ping"].Object.GetPingdest().Interval < 100 {
							client.Logging.Warnf("monclient:%v, interval for ping object:%v is too low", monitoringObject.GetPingdest().GetDestination(), client.MonObecjts[monitoringObject.GetPingdest().GetDestination()+"-ping"].Object.GetPingdest().Interval)
							client.MonObecjts[monitoringObject.GetPingdest().GetDestination()+"-ping"].Object.GetPingdest().Interval = 100
						} else if client.MonObecjts[monitoringObject.GetPingdest().GetDestination()+"-ping"].Object.GetPingdest().Interval > 60000 {
							client.Logging.Warnf("monclient:%v, interval for ping object:%v is too high", monitoringObject.GetPingdest().GetDestination(), client.MonObecjts[monitoringObject.GetPingdest().GetDestination()+"-ping"].Object.GetPingdest().Interval)
							client.MonObecjts[monitoringObject.GetPingdest().GetDestination()+"-ping"].Object.GetPingdest().Interval = 60000
						}
					case *proto.MonitoringObject_Resolvedest:
						client.MonObecjts[monitoringObject.GetResolvedest().GetDestination()+"-resolve"] = &nmonclient.MonObject{
							ConfigurationUpdatetime: time.Now().UnixNano(),
							Object:                  monitoringObject,
						}
						// Configuration checks
						// Check interval
						if client.MonObecjts[monitoringObject.GetResolvedest().GetDestination()+"-resolve"].Object.GetResolvedest().Interval < 3000 {
							client.MonObecjts[monitoringObject.GetResolvedest().GetDestination()+"-resolve"].Object.GetResolvedest().Interval = 3000
							client.Logging.Warnf("monclient:%v, interval for resolve object:%v is too low", monitoringObject.GetResolvedest().GetDestination(), client.MonObecjts[monitoringObject.GetResolvedest().GetDestination()+"-resolve"].Object.GetResolvedest().Interval)
						} else if client.MonObecjts[monitoringObject.GetResolvedest().GetDestination()+"-resolve"].Object.GetResolvedest().Interval > 60000 {
							client.Logging.Warnf("monclient:%v, interval for resolve object:%v is too high", monitoringObject.GetResolvedest().GetDestination(), client.MonObecjts[monitoringObject.GetResolvedest().GetDestination()+"-resolve"].Object.GetResolvedest().Interval)
							client.MonObecjts[monitoringObject.GetResolvedest().GetDestination()+"-resolve"].Object.GetResolvedest().Interval = 60000
						}
					case *proto.MonitoringObject_Tracedest:
						client.MonObecjts[monitoringObject.GetTracedest().GetDestination()+"-trace"] = &nmonclient.MonObject{
							ConfigurationUpdatetime: time.Now().UnixNano(),
							Object:                  monitoringObject,
						}
						// Configuration checks
						// Check interval
						if client.MonObecjts[monitoringObject.GetTracedest().GetDestination()+"-trace"].Object.GetTracedest().Interval < 60000 {
							client.MonObecjts[monitoringObject.GetTracedest().GetDestination()+"-trace"].Object.GetTracedest().Interval = 60000
							client.Logging.Warnf("monclient:%v, interval for trace object:%v is too low", monitoringObject.GetTracedest().GetDestination(), client.MonObecjts[monitoringObject.GetTracedest().GetDestination()+"-trace"].Object.GetTracedest().Interval)
						} else if client.MonObecjts[monitoringObject.GetTracedest().GetDestination()+"-trace"].Object.GetTracedest().Interval > 1800000 {
							client.Logging.Warnf("monclient:%v, interval for trace object:%v is too high", monitoringObject.GetTracedest().GetDestination(), client.MonObecjts[monitoringObject.GetTracedest().GetDestination()+"-trace"].Object.GetTracedest().Interval)
							client.MonObecjts[monitoringObject.GetTracedest().GetDestination()+"-trace"].Object.GetTracedest().Interval = 1800000
						}
					case nil:
						// The field is not set.
					default:
						client.Logging.Errorf("unexpected monitoring object type %T", t)
					}
				}
			}(stream)
		}
		client.Logging.Errorf("reading configuration data failed: %v, waiting for 10sec to retry")
		time.Sleep(10 * time.Second)
	}
}
func connectStatsServer(client *nmonclient.NmonClient) {
	client.Logging.Tracef("trying to connect stats server:%v", statsServer)
	for {
		if !client.IsStatsClientConnected {
			client.Logging.Infof("tring to connect statistic server:%v with name:%v and id:%v", *statsServer, client.StatsClient.GetName(), client.StatsClient.GetId())
			conn, err := grpc.Dial(*statsServer, grpc.WithInsecure(), grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                1 * time.Second, // send pings every 10 seconds if there is no activity
				Timeout:             time.Second,     // wait 1 second for ping ack before considering the connection dead
				PermitWithoutStream: true,            // send pings even without active streams
			}), grpc.WithDefaultServiceConfig(`{
				"methodConfig": [{
				  "name": [{"service": "nmon client service"}],
				  "waitForReady": true,
				  "retryPolicy": {
					  "MaxAttempts": 4,
					  "InitialBackoff": "1s",
					  "MaxBackoff": "6s",
					  "BackoffMultiplier": 1.5,
					  "RetryableStatusCodes": [ "UNAVAILABLE" ]
				  }
				}]}`), grpc.WithBlock())
			if err != nil {
				client.Logging.Errorf("could not connect to statistic service:%v, waiting for 10sec to retry", err)
				time.Sleep(10 * time.Second)
				client.IsStatsClientConnected = false
				break
			}
			client.IsStatsClientConnected = true
			client.Logging.Infof("connected to the statistic server:%v with name:%v and id:%v", *statsServer, client.StatsClient.GetName(), client.StatsClient.GetId())
			client.StatsConnClient = proto.NewStatsClient(conn)
			stream, err := client.StatsConnClient.RecordStats(context.Background())
			if err != nil {
				//TODO: changling log
				client.Logging.Errorf("statistic service registration failed:%v, waiting for 10sec to retry", err)
				time.Sleep(10 * time.Second)
				break
			}
			for {
				msg := &proto.StatsObject{
					Client:    client.StatsClient,
					Timestamp: time.Now().UnixNano(),
					Object: &proto.StatsObject_Clientstat{
						Clientstat: &proto.ClientStat{
							NumberOfMonObjects: int32(len(client.MonObecjts)),
						},
					},
				}
				if err := stream.Send(msg); err != nil {
					client.Logging.Errorf("can not send client stats:%v, err:%v", stream, err)
					time.Sleep(1 * time.Second)
					client.IsStatsClientConnected = false
					break
				}
				client.Logging.Infof("sent client stats update to the server, timestamp:%v and number of monitoring objects is:%v", msg.GetTimestamp(), int32(len(client.MonObecjts)))
				time.Sleep(1 * time.Second)
			}
			client.Logging.Errorf("sending client stats data failed: %v, waiting for 10sec to retry")
			time.Sleep(10 * time.Second)
		}
	}
}
func main() {
	client.Logging.Infof("client is initialized with parameters:%v", client)
	go getMonitoringObjects(client)
	go connectStatsServer(client)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs)
	go func() {
		s := <-sigs
		switch s {
		case syscall.SIGURG:
			client.Logging.Infof("received unhandled %v signal from os:", s)
		default:
			client.Logging.Infof("received %v signal from os,exiting", s)
			os.Exit(1)
		}
	}()
	client.Run()
	client.WaitGroup.Add(1)
	go func() {
		defer client.WaitGroup.Done()
	}()
	go func() {
		client.WaitGroup.Wait()
	}()
	select {}
}
