package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	api "github.com/denizaydin/nmon/api"
	proto "github.com/denizaydin/nmon/api"
	"github.com/fsnotify/fsnotify"
	logrus "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
)

//server - configuration server
var server *Server

//MonitoringObjects - Objects to be monitored, ping, trace, resolve which are defined in the proto file.
type MonitoringObjects struct {
	MonitorObjects *map[string]*proto.MonitoringObject
}

//ClientConnection - Client connection
type ClientConnection struct {
	stream         proto.ConfigServer_CreateStreamServer
	client         proto.Client
	net            string
	active         bool
	lastactivetime int64
	error          chan error
}

//Server - Holds varibles of the server
type Server struct {
	//ServerAddr - net address
	ServerAddr string
	//Current monitoring objects
	MonitorObjects map[string]*proto.MonitoringObject
	//Current connected clients
	Connections map[string]*ClientConnection
	//Client update time. Current monitoring object will be sent to the clients at this period.
	//We do not any special message to client to remove deleted ones. Client supposed to watch this data and kill the monitoring if its not updated within maxruntime variable of monitoring object.
	ClientUpdateTime int
	//Data file name
	DataFileName string
	//Data file path
	DataFilePath string
	Logging      *logrus.Logger
}

func init() {
	//Create new server instance
	server = &Server{
		MonitorObjects:   map[string]*proto.MonitoringObject{},
		Connections:      map[string]*ClientConnection{},
		ClientUpdateTime: 5,
		Logging:          &logrus.Logger{},
	}
	server.MonitorObjects = make(map[string]*proto.MonitoringObject)
	server.Logging = logrus.New()
	// Log as JSON instead of the default ASCII formatter.
	server.Logging.SetFormatter(&logrus.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})
	//TODO: add a flag for logging to file or any other method
	//Output to stdout instead of the default stderr
	//Can be any io.Writer, see below for File example
	server.Logging.SetOutput(os.Stdout)
	// Only log the warning severity or above.
	logLevel := "info"
	flag.StringVar(&logLevel, "loglevel", "info", "disable,info, error, warning,debug or trace")
	flag.StringVar(&server.ServerAddr, "addr", "localhost:8081", "server net address")
	flag.IntVar(&server.ClientUpdateTime, "updatetime", 5, "configuration update time in seconds, use lower values for better results as we are using grpc streaming")
	flag.StringVar(&server.DataFileName, "datafilename", "dataConfig.json", "monitoring objects data file name as json")
	flag.StringVar(&server.DataFilePath, "datafilepath", ".", "monitoring objects data file path, default is current directory")
	flag.Parse()
	switch logLevel {
	case "disable":
		server.Logging.SetOutput(ioutil.Discard)
	case "info":
		server.Logging.SetLevel(logrus.InfoLevel)
	case "error":
		server.Logging.SetLevel(logrus.ErrorLevel)
	case "warn":
		server.Logging.SetLevel(logrus.WarnLevel)
	case "debug":
		server.Logging.SetLevel(logrus.DebugLevel)
	case "trace":
		server.Logging.SetLevel(logrus.TraceLevel)
	default:
		server.Logging.SetLevel(logrus.DebugLevel)
	}
}

// CreateStream - Creates a connection, appends the connection to the servers connetion slice, and returns a channel error
func (s *Server) CreateStream(pconn *api.Connect, stream api.ConfigServer_CreateStreamServer) error {
	pr, ok := peer.FromContext(stream.Context())
	if !ok {
		server.Logging.Warningf("configserver: cannot get client ip address!! %v", pr.Addr)
	}
	server.Logging.Debugf("configserver: received client connection request from ip %v", pr.Addr)
	//Parsing client's ip address
	clientIP, _, _ := net.SplitHostPort(pr.Addr.String())
	clientName := pconn.Client.GetName()
	if clientName == "" {
		server.Logging.Warnf("configserver: client name is empty, using client ip address:%v as its name", clientIP)
		return fmt.Errorf("configserver: client name cannot be empty")
	}
	server.Logging.Infof("configserver: registering or updating client:%v, groups:%v  as ping:%v, as trace:%v, as app:%v", pconn.Client.GetName(), pconn.Client.GetGroups(), pconn.Client.GetAddAsPingDest(), pconn.Client.GetAddAsTraceDest(), pconn.Client.GetAddAsAppDest())
	// may be reject client requests from the same ip address!
	s.Connections[clientName] = &ClientConnection{
		stream:         stream,
		client:         *pconn.Client,
		net:            pr.Addr.String(),
		active:         true,
		lastactivetime: time.Now().UnixNano(),
		error:          make(chan error),
	}
	return <-s.Connections[pconn.Client.GetName()].error
}

//removeLongDeadClients - checks the current connections for clients which are inactive more than 4 updata time interval.
func removeLongDeadClients(s *Server) {
	go func() {
		for {
			time.Sleep(4 * time.Duration(s.ClientUpdateTime) * time.Second)
			s.Logging.Debug("configserver: checking for connections which are not active more than 4 times update period:%v", s.ClientUpdateTime)
			for key, conn := range s.Connections {
				if conn.active == false {
					server.Logging.Debugf("configserver: found inactive client:%v, last active time:%v and our time:%v", key, time.Unix(0, conn.lastactivetime), time.Now())
					if time.Now().UnixNano()-conn.lastactivetime > int64(time.Duration(4*s.ClientUpdateTime*int(time.Second))) {
						delete(s.Connections, key)
						server.Logging.Infof("found inactive client:%v, removed from connection list", conn.client)
					}
				}
			}
		}
	}()
}

func calculateUpdate(s *Server, updateClient *ClientConnection) map[string]*proto.MonitoringObject {
	var update = make(map[string]*proto.MonitoringObject)
	for key, conn := range s.Connections {
		s.Logging.Debugf("configserver: checking if client:%v wants to be monitored, as ping:%v, as trace:%v, as app:%v", key, conn.client.GetAddAsPingDest(), conn.client.GetAddAsTraceDest(), conn.client.GetAddAsAppDest())
		if conn.client.AddAsAppDest || conn.client.AddAsTraceDest || conn.client.AddAsAppDest {
			s.Logging.Tracef("configserver: client:%v wants to be monitored, as ping:%v, as trace:%v, as app:%v", conn.client.GetAddAsPingDest(), conn.client.GetAddAsTraceDest(), conn.client.GetAddAsAppDest())
		}
		if !conn.active {
			s.Logging.Tracef("configserver: cheking client:%v, conn name:%v is inactive,passing", conn.client.Name)
			continue
		}
		if updateClient.client.Name == conn.client.Name {
			s.Logging.Tracef("configserver: can not send client info to the same client")
			continue
		}
		//checking if one of the client object groups will match one of clients groups which update will be send
		if func(g1 map[string]string, g2 map[string]string) bool {
			for group := range g1 {
				for updateClientGroup := range g2 {
					s.Logging.Tracef("configserver: cheking client:%v group:%v, update client group:%v", key, group, updateClientGroup)
					if group == updateClientGroup {
						s.Logging.Tracef("configserver: cheking client:%v group:%v with, matched update client group:%v", key, group, updateClientGroup)
						return true
					}
				}
			}
			return false
		}(conn.client.Groups, updateClient.client.Groups) {
			if conn.client.GetAddAsPingDest() {
				server.Logging.Tracef("configserver: cheking client:%v, wants to the monitoried by ping", key, conn.client.Name)
				clientIP, _, _ := net.SplitHostPort(conn.net)
				update[conn.client.Id] = &proto.MonitoringObject{
					Updatetime: time.Now().UnixNano(),
					Object: &proto.MonitoringObject_Pingdest{
						Pingdest: &proto.PingDest{
							Destination: clientIP,
							Timeout:     int32(3 * s.ClientUpdateTime),
							Interval:    1000000000,
							PacketSize:  9000,
							Groups:      map[string]string{},
						},
					},
				}
				server.Logging.Tracef("configserver: cheking client:%v, added ip address:%v client name:%v id:%v to the monitoring list as a ping destination", key, conn.client.Name, conn.client.Id)

			}
		}
	}
	for key, monObject := range s.MonitorObjects {
		monObject.Updatetime = time.Now().UnixNano()
		switch monObject.Object.(type) {
		case *proto.MonitoringObject_Pingdest:
			//checking if one of the monitoring object groups will match one of the clients groups
			if func(g1 map[string]string, g2 map[string]string) bool {
				for group := range g1 {
					for conngroup := range g2 {
						s.Logging.Tracef("configserver: checking ping Object:%v group with update client group:%v", group, conngroup)
						if group == conngroup {
							s.Logging.Tracef("configserver: checking ping Object:%v, matched update client group:%v", group, conngroup)
							return true
						}
					}
				}
				return false
			}(monObject.GetPingdest().Groups, updateClient.client.Groups) {
				s.Logging.Tracef("configserver: checking ping Object:%v, object timeout to:%v", key, int32(3*s.ClientUpdateTime))
				update[key] = &proto.MonitoringObject{
					Updatetime: time.Now().UnixNano(),
					Object: &proto.MonitoringObject_Pingdest{
						Pingdest: &proto.PingDest{
							Destination: monObject.GetPingdest().Destination,
							Timeout:     int32(3 * s.ClientUpdateTime),
							Interval:    monObject.GetPingdest().Interval,
							PacketSize:  monObject.GetPingdest().PacketSize,
						},
					},
				}
			}
		case *proto.MonitoringObject_Resolvedest:
			//checking if one of the monitoring object groups will match one of the clients groups
			if func(g1 map[string]string, g2 map[string]string) bool {
				for group := range g1 {
					for conngroup := range g2 {
						s.Logging.Tracef("configserver: checking resolve Object:%v group with update client group:%v", group, conngroup)
						if group == conngroup {
							s.Logging.Tracef("configserver: checking resolve Object:%v, matched update client group:%v", group, conngroup)
							return true
						}
					}
				}
				return false
			}(monObject.GetResolvedest().Groups, updateClient.client.Groups) {
				s.Logging.Tracef("configserver: checking resolve Object:%v, object timeout to:%v", key, int32(3*s.ClientUpdateTime))
				update[key] = &proto.MonitoringObject{
					Updatetime: time.Now().UnixNano(),
					Object: &proto.MonitoringObject_Resolvedest{
						Resolvedest: &proto.ResolveDest{
							Destination:   monObject.GetResolvedest().Destination,
							Timeout:       int32(3 * s.ClientUpdateTime),
							Interval:      monObject.GetResolvedest().Interval,
							ResolveServer: monObject.GetResolvedest().ResolveServer,
						},
					},
				}
			}
		case *proto.MonitoringObject_Tracedest:
			//checking if one of the monitoring object groups will match one of the clients groups
			if func(g1 map[string]string, g2 map[string]string) bool {
				for group := range g1 {
					for conngroup := range g2 {
						s.Logging.Tracef("configserver: checking trace Object:%v group with update client group:%v", group, conngroup)
						if group == conngroup {
							s.Logging.Tracef("configserver: checking trace Object:%v, matched update client group:%v", group, conngroup)
							return true
						}
					}
				}
				return false
			}(monObject.GetTracedest().Groups, updateClient.client.Groups) {
				s.Logging.Tracef("configserver: checking resolve Object:%v, object timeout to:%v", key, int32(3*s.ClientUpdateTime))
				update[key] = &proto.MonitoringObject{
					Updatetime: time.Now().UnixNano(),
					Object: &proto.MonitoringObject_Tracedest{
						Tracedest: &proto.TraceDest{
							Destination: monObject.GetTracedest().Destination,
							Timeout:     int32(3 * s.ClientUpdateTime),
							Interval:    monObject.GetTracedest().Interval,
						},
					},
				}
			}
		}

		s.Logging.Debugf("configserver: update for the monitoring object:%v to the client:%v", update, updateClient.client.GetName())
	}
	return update
}

//broadcastData - Broadcasts configuration data to the connected clients
func broadcastData(s *Server) {
	ticker := time.NewTicker(time.Duration(s.ClientUpdateTime) * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				s.Logging.Tracef("configserver: update time, number of connections for sending update:%v", len(s.Connections))
				//Check the registered clients
				for _, conn := range s.Connections {
					s.Logging.Debugf("configserver: sending update to the client:%v on net address:%v", conn.client.GetName(), conn.net)
					for key, monObject := range calculateUpdate(s, conn) {
						go func(monObject *proto.MonitoringObject, conn *ClientConnection) {
							if conn.active {
								err := conn.stream.Send(monObject)
								if err != nil {
									s.Logging.Errorf("error:%v with stream:%s for client:%v address:%v", err, conn.stream, conn.client.GetName(), conn.net)
									conn.active = false
									conn.error <- err
								}
								s.Logging.Infof("configserver: sent update, monitoring object:%v to the client:%v", key, conn.client.GetName())
							}
						}(monObject, conn)
					}
				}
			}
		}
	}()
}

//getData - retrive configuration data periodically from a file
func getData(s *Server) {
	monitoringObjects := make(map[string]*proto.MonitoringObject)
	pingdestinations := make(map[string]*proto.PingDest)
	tracedestinations := make(map[string]*proto.TraceDest)
	resolvedestinations := make(map[string]*proto.ResolveDest)
	// Set the file name of the configurations file
	viper.SetConfigName(s.DataFileName)
	// Set the path to look for the configurations file
	viper.AddConfigPath(s.DataFilePath)
	viper.SetConfigType("json")
	viper.WatchConfig()
	viper.OnConfigChange(func(e fsnotify.Event) {
		server.Logging.Infof("config file changed:%v", e.Name)
		newmonitoringObjects := make(map[string]*proto.MonitoringObject)

		pingdestinations = make(map[string]*proto.PingDest)
		tracedestinations = make(map[string]*proto.TraceDest)
		resolvedestinations = make(map[string]*proto.ResolveDest)

		viper.UnmarshalKey("pingdests", &pingdestinations)
		viper.UnmarshalKey("tracedests", &tracedestinations)
		viper.UnmarshalKey("resolvedests", &resolvedestinations)

		for pingDest := range pingdestinations {
			newmonitoringObjects[pingDest+"-ping"] = &proto.MonitoringObject{
				Object: &proto.MonitoringObject_Pingdest{
					Pingdest: pingdestinations[pingDest],
				},
			}
			if pingdestinations[pingDest].Timeout <= 0 && (pingdestinations[pingDest].Timeout < 3*10000000*pingdestinations[pingDest].Interval) {
				server.Logging.Warnf("configserver: changing pingdest:%v timeout value to 3 times interval:%v value", pingdestinations[pingDest].Timeout, pingdestinations[pingDest].Interval)
				newmonitoringObjects[pingDest+"-ping"].GetPingdest().Timeout = 3 * pingdestinations[pingDest].Interval * 10000000
			}
			newmonitoringObjects[pingDest+"-ping"].GetPingdest().Name = pingDest

		}
		for traceDest := range tracedestinations {
			newmonitoringObjects[traceDest+"-trace"] = &proto.MonitoringObject{
				Object: &proto.MonitoringObject_Tracedest{
					Tracedest: tracedestinations[traceDest],
				},
			}
			if tracedestinations[traceDest].Timeout <= 0 && (tracedestinations[traceDest].Timeout < 3*10000000*tracedestinations[traceDest].Interval) {
				server.Logging.Warnf("configserver: changing tracedest:%v timeout value to 3 times interval:%v value", tracedestinations[traceDest].Timeout, tracedestinations[traceDest].Interval)
				newmonitoringObjects[traceDest+"-trace"].GetTracedest().Timeout = 3 * tracedestinations[traceDest].Interval * 10000000
			}
			newmonitoringObjects[traceDest+"-trace"].GetTracedest().Name = traceDest

		}
		for resolveDest := range resolvedestinations {
			newmonitoringObjects[resolveDest+"-resolve"] = &proto.MonitoringObject{
				Object: &proto.MonitoringObject_Resolvedest{
					Resolvedest: resolvedestinations[resolveDest],
				},
			}
			if resolvedestinations[resolveDest].Timeout <= 0 && (resolvedestinations[resolveDest].Timeout < 3*10000000*resolvedestinations[resolveDest].Interval) {
				server.Logging.Warnf("configserver: changing resolveDest:%v timeout value to 3 times interval:%v value", resolvedestinations[resolveDest].Timeout, resolvedestinations[resolveDest].Interval)
				newmonitoringObjects[resolveDest+"-resolve"].GetResolvedest().Timeout = 3 * resolvedestinations[resolveDest].Interval * 10000000
			}
			newmonitoringObjects[resolveDest+"-resolve"].GetResolvedest().Name = resolveDest

		}
		server.MonitorObjects = newmonitoringObjects
		server.Logging.Debugf("configserver: changed configuration data to %v", newmonitoringObjects)
	})
	server.Logging.Infof("configserver: using config: %s\n", viper.ConfigFileUsed())
	for {
		if err := viper.ReadInConfig(); err != nil {
			server.Logging.Errorf("configserver: can not read config file, retring in 10sec, %s", err)
			time.Sleep(10 * time.Second)
		} else {
			server.Logging.Debugf("configserver: read config file")
			break
		}
	}
	viper.UnmarshalKey("pingdests", &pingdestinations)
	viper.UnmarshalKey("tracedests", &tracedestinations)
	viper.UnmarshalKey("resolvedests", &resolvedestinations)
	for pingDest := range pingdestinations {
		monitoringObjects[pingDest+"-ping"] = &proto.MonitoringObject{
			Object: &proto.MonitoringObject_Pingdest{
				Pingdest: pingdestinations[pingDest],
			},
		}
		if pingdestinations[pingDest].Timeout <= 0 && (pingdestinations[pingDest].Timeout < 3*10000000*pingdestinations[pingDest].Interval) {
			server.Logging.Warnf("configserver: changing pingdest:%v timeout value to 3 times interval:%v value", pingdestinations[pingDest].Timeout, pingdestinations[pingDest].Interval)
			monitoringObjects[pingDest+"-ping"].GetPingdest().Timeout = 3 * pingdestinations[pingDest].Interval * 10000000
		}
		monitoringObjects[pingDest+"-ping"].GetPingdest().Name = pingDest
	}
	for traceDest := range tracedestinations {
		monitoringObjects[traceDest+"-trace"] = &proto.MonitoringObject{
			Updatetime: 0,
			Object:     &proto.MonitoringObject_Tracedest{Tracedest: tracedestinations[traceDest]},
		}
		if tracedestinations[traceDest].Timeout <= 0 && (tracedestinations[traceDest].Timeout < 3*10000000*tracedestinations[traceDest].Interval) {
			server.Logging.Warnf("configserver: changing tracedest:%v timeout value to 3 times interval:%v value", tracedestinations[traceDest].Timeout, tracedestinations[traceDest].Interval)
			monitoringObjects[traceDest+"-trace"].GetTracedest().Timeout = 3 * tracedestinations[traceDest].Interval * 10000000
		}
		monitoringObjects[traceDest+"-trace"].GetTracedest().Name = traceDest

	}
	for resolveDest := range resolvedestinations {
		monitoringObjects[resolveDest+"-resolve"] = &proto.MonitoringObject{
			Object: &proto.MonitoringObject_Resolvedest{
				Resolvedest: resolvedestinations[resolveDest],
			},
		}
		if resolvedestinations[resolveDest].Timeout <= 0 && (resolvedestinations[resolveDest].Timeout < 3*10000000*resolvedestinations[resolveDest].Interval) {
			server.Logging.Warnf("configserver: changing resolveDest:%v timeout value to 3 times interval:%v value", resolvedestinations[resolveDest].Timeout, resolvedestinations[resolveDest].Interval)
			monitoringObjects[resolveDest+"-resolve"].GetResolvedest().Timeout = 3 * resolvedestinations[resolveDest].Interval * 10000000
		}
		monitoringObjects[resolveDest+"-resolve"].GetResolvedest().Name = resolveDest

	}
	server.Logging.Debugf("configserver: returning configuration data %v", monitoringObjects)
	server.MonitorObjects = monitoringObjects
}
func main() {
	server.Logging.Infof("configserver: server is initialized with parameters:%+v", server)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs)
	go func() {
		s := <-sigs
		switch s {
		case syscall.SIGURG:
			server.Logging.Infof("received unhandled %v signal from os:", s)
		default:
			server.Logging.Infof("received %v signal from os,exiting", s)
			os.Exit(1)
		}
	}()

	go getData(server)
	grpcServer := grpc.NewServer(grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
		MinTime:             5 * time.Second, // If a client pings more than once every 5 seconds, terminate the connection
		PermitWithoutStream: true,            // Allow pings even when there are no active streams
	}), grpc.KeepaliveParams(keepalive.ServerParameters{
		MaxConnectionIdle: 15 * time.Second, // If a client is idle for 15 seconds, send a GOAWAY
		Time:              5 * time.Second,  // Ping the client if it is idle for 5 seconds to ensure the connection is still active
		Timeout:           1 * time.Second,  // Wait 1 second for the ping ack before assuming the connection is dead
	}))
	server.Logging.Debugf("configserver: got the configuration data:%v", server.MonitorObjects)
	server.Logging.Infof("configserver: starting server at:%v", server.ServerAddr)
	listener, err := net.Listen("tcp", server.ServerAddr)
	if err != nil {
		server.Logging.Fatalf("configserver: error creating the server %v", err)
	}
	go broadcastData(server)
	go removeLongDeadClients(server)
	proto.RegisterConfigServerServer(grpcServer, server)
	server.Logging.Infof("configserver: started server at:%v", listener.Addr())
	grpcServer.Serve(listener)
	select {}
}
