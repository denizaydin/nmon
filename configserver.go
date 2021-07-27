package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	api "dnzydn.com/nmon/api"
	proto "dnzydn.com/nmon/api"
	"dnzydn.com/nmon/configserver"
	"github.com/fsnotify/fsnotify"
	logrus "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
)

//Flag variables
//srvNetAddr - Server adress
var srvNetAddr string

//srvNetPort - Server port
var srvNetPort int

//updateTime - int
var updateTime int

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
	//Current monitoring objects
	MonitorObjects map[string]*proto.MonitoringObject
	//Current connected clients
	Connections map[string]*ClientConnection
	//Client update time. Current monitoring object will be sent to the clients at this period.
	//We do not any special message to client to remove deleted ones. Client supposed to watch this data and kill the monitoring if its not updated within maxruntime variable of monitoring object.
	ClientUpdateRefreshTime int
	Logging                 *logrus.Logger
}

func init() {
	//Create new server instance
	server = &Server{
		MonitorObjects:          map[string]*proto.MonitoringObject{},
		Connections:             map[string]*ClientConnection{},
		ClientUpdateRefreshTime: updateTime,
		Logging:                 &logrus.Logger{},
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
	flag.StringVar(&logLevel, "loglevel", "info", "info, error, warning,debug or trace")
	flag.StringVar(&srvNetAddr, "ipaddr", "127.0.0.0", "Server Net Address")
	flag.IntVar(&srvNetPort, "port", 8080, "Server Net Port")
	flag.IntVar(&server.ClientUpdateRefreshTime, "updatetime", 5, "configuration update time in seconds, use lower values for better results as we are using grpc streaming")
	flag.Parse()
	switch logLevel {
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
	if configserver.CheckIPAddress(srvNetAddr) {
		server.Logging.Fatalf("Invalid ip address:%v", srvNetAddr)
	}
}

// CreateStream - Creates a connection, appends the connection to the servers connetion slice, and returns a channel error
func (s *Server) CreateStream(pconn *api.Connect, stream api.ConfigServer_CreateStreamServer) error {
	pr, ok := peer.FromContext(stream.Context())
	if !ok {
		server.Logging.Warningf("cannot get client ip address!! %v", pr.Addr)
	}
	server.Logging.Infof("received client connection request from ip %v", pr.Addr)
	//Parsing client's ip address
	clientIP, _, _ := net.SplitHostPort(pr.Addr.String())
	clientName := pconn.Client.GetName()
	if clientName == "" {
		server.Logging.Warnf("client name is empty, using client ip address:%v as its name", clientIP)
		return fmt.Errorf("client name cannot be empty")
	}
	server.Logging.Infof("registering or updating client:%v, groups:%v  as ping:%v, as trace:%v, as app:%v", pconn.Client.GetName(), pconn.Client.GetGroups(), pconn.Client.GetAddAsPingDest(), pconn.Client.GetAddAsTraceDest(), pconn.Client.GetAddAsAppDest())
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
			time.Sleep(4 * time.Duration(s.ClientUpdateRefreshTime) * time.Second)
			s.Logging.Debug("checking for connections which are not active more than 4 times update period:%v", s.ClientUpdateRefreshTime)
			for key, conn := range s.Connections {
				if conn.active == false {
					server.Logging.Infof("found inactive client:%v, last active time:%v and our time:%v", key, time.Unix(0, conn.lastactivetime), time.Now())
					if time.Now().UnixNano()-conn.lastactivetime > int64(time.Duration(4*s.ClientUpdateRefreshTime*int(time.Second))) {
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
		s.Logging.Debugf("checking if client:%v wants to be monitored, as ping:%v, as trace:%v, as app:%v", key, conn.client.GetAddAsPingDest(), conn.client.GetAddAsTraceDest(), conn.client.GetAddAsAppDest())
		if conn.client.AddAsAppDest || conn.client.AddAsTraceDest || conn.client.AddAsAppDest {
			s.Logging.Infof("client:%v wants to be monitored, as ping:%v, as trace:%v, as app:%v", conn.client.GetAddAsPingDest(), conn.client.GetAddAsTraceDest(), conn.client.GetAddAsAppDest())
		}
		if !conn.active {
			s.Logging.Debugf("cheking client:%v, conn name:%v is inactive,passing", conn.client.Name)
			continue
		}
		if updateClient.client.Name == conn.client.Name {
			s.Logging.Debugf("can not send client info to the same client")
			continue
		}
		//checking if one of the client object groups will match one of clients groups which update will be send
		if func(g1 map[string]string, g2 map[string]string) bool {
			for group := range g1 {
				for updateClientGroup := range g2 {
					s.Logging.Tracef("cheking client:%v group:%v, update client group:%v", key, group, updateClientGroup)
					if group == updateClientGroup {
						s.Logging.Debugf("cheking client:%v group:%v with, matched update client group:%v", key, group, updateClientGroup)
						return true
					}
				}
			}
			return false
		}(conn.client.Groups, updateClient.client.Groups) {
			if conn.client.GetAddAsPingDest() {
				server.Logging.Infof("cheking client:%v, wants to the monitoried by ping", key, conn.client.Name)
				clientIP, _, _ := net.SplitHostPort(conn.net)
				if configserver.CheckIPAddress(clientIP) {
					server.Logging.Errorf("cheking client:%v, invalid ip address:%v client id:%v, can not add to the monitoring list", key, conn.client.Name, clientIP, conn.client.Id)
					continue
				} else {
					update[conn.client.Id] = &proto.MonitoringObject{
						Updatetime: time.Now().UnixNano(),
						Object: &proto.MonitoringObject_Pingdest{
							Pingdest: &proto.PingDest{
								Destination: clientIP,
								Timeout:     int32(3 * s.ClientUpdateRefreshTime),
								Interval:    1000000000,
								PacketSize:  9000,
								Groups:      map[string]string{},
							},
						},
					}
					server.Logging.Infof("cheking client:%v, added ip address:%v client name:%v id:%v to the monitoring list as a ping destination", key, conn.client.Name, conn.client.Id)
				}
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
						s.Logging.Tracef("checking ping Object:%v group with update client group:%v", group, conngroup)
						if group == conngroup {
							s.Logging.Debugf("checking ping Object:%v, matched update client group:%v", group, conngroup)
							return true
						}
					}
				}
				return false
			}(monObject.GetPingdest().Groups, updateClient.client.Groups) {
				s.Logging.Debugf("checking ping Object:%v, object timeout to:%v", key, int32(3*s.ClientUpdateRefreshTime))
				update[key] = &proto.MonitoringObject{
					Updatetime: time.Now().UnixNano(),
					Object: &proto.MonitoringObject_Pingdest{
						Pingdest: &proto.PingDest{
							Destination: monObject.GetPingdest().Destination,
							Timeout:     int32(3 * s.ClientUpdateRefreshTime),
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
						s.Logging.Tracef("checking resolve Object:%v group with update client group:%v", group, conngroup)
						if group == conngroup {
							s.Logging.Debugf("checking resolve Object:%v, matched update client group:%v", group, conngroup)
							return true
						}
					}
				}
				return false
			}(monObject.GetResolvedest().Groups, updateClient.client.Groups) {
				s.Logging.Debugf("checking resolve Object:%v, object timeout to:%v", key, int32(3*s.ClientUpdateRefreshTime))
				update[key] = &proto.MonitoringObject{
					Updatetime: time.Now().UnixNano(),
					Object: &proto.MonitoringObject_Resolvedest{
						Resolvedest: &proto.ResolveDest{
							Destination:   monObject.GetResolvedest().Destination,
							Timeout:       int32(3 * s.ClientUpdateRefreshTime),
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
						s.Logging.Tracef("checking trace Object:%v group with update client group:%v", group, conngroup)
						if group == conngroup {
							s.Logging.Debugf("checking trace Object:%v, matched update client group:%v", group, conngroup)
							return true
						}
					}
				}
				return false
			}(monObject.GetTracedest().Groups, updateClient.client.Groups) {
				s.Logging.Debugf("checking resolve Object:%v, object timeout to:%v", key, int32(3*s.ClientUpdateRefreshTime))
				update[key] = &proto.MonitoringObject{
					Updatetime: time.Now().UnixNano(),
					Object: &proto.MonitoringObject_Tracedest{
						Tracedest: &proto.TraceDest{
							Destination: monObject.GetTracedest().Destination,
							Timeout:     int32(3 * s.ClientUpdateRefreshTime),
							Interval:    monObject.GetTracedest().Interval,
						},
					},
				}
			}
		}

		s.Logging.Infof("update for the monitoring object:%v to the client:%v", update, updateClient.client.GetName())
	}
	return update
}

//broadcastData - Broadcasts configuration data to the connected clients
func broadcastData(s *Server) {
	ticker := time.NewTicker(time.Duration(s.ClientUpdateRefreshTime) * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				s.Logging.Tracef("update time, number of connections for sending update:%v", len(s.Connections))
				//Check the registered clients
				for _, conn := range s.Connections {
					s.Logging.Debugf("sending update to the client:%v on net address:%v", conn.client.GetName(), conn.net)
					for key, monObject := range calculateUpdate(s, conn) {
						go func(monObject *proto.MonitoringObject, conn *ClientConnection) {
							if conn.active {
								err := conn.stream.Send(monObject)
								if err != nil {
									s.Logging.Errorf("error:%v with stream:%s for client:%v address:%v", err, conn.stream, conn.client.GetName(), conn.net)
									conn.active = false
									conn.error <- err
								}
								s.Logging.Infof("sent monitoring object:%v to the client:%v", key, conn.client.GetName())
							}
						}(monObject, conn)
					}
				}
			}
		}
	}()
}
func getData(s *Server) {
	monitoringObjects := make(map[string]*proto.MonitoringObject)
	pingdestinations := make(map[string]*proto.PingDest)
	tracedestinations := make(map[string]*proto.TraceDest)
	resolvedestinations := make(map[string]*proto.ResolveDest)
	// Set the file name of the configurations file
	viper.SetConfigName("dataConfig.json")
	// Set the path to look for the configurations file
	viper.AddConfigPath("configserver/dataconfig")
	viper.SetConfigType("json")
	viper.WatchConfig()
	viper.OnConfigChange(func(e fsnotify.Event) {
		server.Logging.Println("config file changed:%v", e.Name)
		newmonitoringObjects := make(map[string]*proto.MonitoringObject)

		pingdestinations = make(map[string]*proto.PingDest)
		tracedestinations = make(map[string]*proto.TraceDest)
		resolvedestinations = make(map[string]*proto.ResolveDest)

		viper.UnmarshalKey("pingdests", &pingdestinations)
		viper.UnmarshalKey("tracedests", &tracedestinations)
		viper.UnmarshalKey("resolvedests", &resolvedestinations)

		for pingDest := range pingdestinations {
			newmonitoringObjects[pingDest] = &proto.MonitoringObject{
				Object: &proto.MonitoringObject_Pingdest{
					Pingdest: pingdestinations[pingDest],
				},
			}
		}
		for traceDest := range tracedestinations {
			newmonitoringObjects[traceDest] = &proto.MonitoringObject{
				Object: &proto.MonitoringObject_Tracedest{
					Tracedest: tracedestinations[traceDest],
				},
			}
		}
		for resolveDest := range resolvedestinations {
			newmonitoringObjects[resolveDest] = &proto.MonitoringObject{
				Object: &proto.MonitoringObject_Resolvedest{
					Resolvedest: resolvedestinations[resolveDest],
				},
			}
		}
		server.MonitorObjects = newmonitoringObjects
		server.Logging.Tracef("changed configuration data to %v", monitoringObjects)
	})
	server.Logging.Infof("using config: %s\n", viper.ConfigFileUsed())
	if err := viper.ReadInConfig(); err != nil {
		server.Logging.Errorf("can not read config file, %s", err)
	}
	viper.UnmarshalKey("pingdests", &pingdestinations)
	viper.UnmarshalKey("tracedests", &tracedestinations)
	viper.UnmarshalKey("resolvedests", &resolvedestinations)
	for pingDest := range pingdestinations {

		monitoringObjects[pingDest] = &proto.MonitoringObject{
			Object: &proto.MonitoringObject_Pingdest{
				Pingdest: pingdestinations[pingDest],
			},
		}
		if pingdestinations[pingDest].Timeout <= 0 && (pingdestinations[pingDest].Timeout < 3*10000000*pingdestinations[pingDest].Interval) {
			server.Logging.Warnf("changing pingdest:%v timeout value to 3 times interval:%v value", pingdestinations[pingDest].Timeout, pingdestinations[pingDest].Interval)
			monitoringObjects[pingDest].GetPingdest().Timeout = 3 * pingdestinations[pingDest].Interval * 10000000
		}

	}
	for traceDest := range tracedestinations {
		monitoringObjects[traceDest] = &proto.MonitoringObject{
			Updatetime: 0,
			Object:     &proto.MonitoringObject_Tracedest{Tracedest: tracedestinations[traceDest]},
		}
		if tracedestinations[traceDest].Timeout <= 0 && (tracedestinations[traceDest].Timeout < 3*10000000*tracedestinations[traceDest].Interval) {
			server.Logging.Warnf("changing tracedest:%v timeout value to 3 times interval:%v value", tracedestinations[traceDest].Timeout, tracedestinations[traceDest].Interval)
			monitoringObjects[traceDest].GetTracedest().Timeout = 3 * tracedestinations[traceDest].Interval * 10000000
		}
	}
	for resolveDest := range resolvedestinations {
		monitoringObjects[resolveDest] = &proto.MonitoringObject{
			Object: &proto.MonitoringObject_Resolvedest{
				Resolvedest: resolvedestinations[resolveDest],
			},
		}
		if resolvedestinations[resolveDest].Timeout <= 0 && (resolvedestinations[resolveDest].Timeout < 3*10000000*resolvedestinations[resolveDest].Interval) {
			server.Logging.Warnf("changing resolveDest:%v timeout value to 3 times interval:%v value", resolvedestinations[resolveDest].Timeout, resolvedestinations[resolveDest].Interval)
			monitoringObjects[resolveDest].GetResolvedest().Timeout = 3 * resolvedestinations[resolveDest].Interval * 10000000
		}
	}
	server.Logging.Tracef("returning configuration data %v", monitoringObjects)

	server.MonitorObjects = monitoringObjects
}
func main() {
	server.Logging.Infof("server is initialized with parameters:%+v", server)
	go getData(server)
	grpcServer := grpc.NewServer(grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
		MinTime:             5 * time.Second, // If a client pings more than once every 5 seconds, terminate the connection
		PermitWithoutStream: true,            // Allow pings even when there are no active streams
	}), grpc.KeepaliveParams(keepalive.ServerParameters{
		MaxConnectionIdle: 15 * time.Second, // If a client is idle for 15 seconds, send a GOAWAY
		Time:              5 * time.Second,  // Ping the client if it is idle for 5 seconds to ensure the connection is still active
		Timeout:           1 * time.Second,  // Wait 1 second for the ping ack before assuming the connection is dead
	}))
	server.Logging.Debugf("got the configuration data:%v", server.MonitorObjects)
	server.Logging.Debug("starting server at port :8080")
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		server.Logging.Fatalf("error creating the server %v", err)
	}
	go broadcastData(server)
	go removeLongDeadClients(server)
	proto.RegisterConfigServerServer(grpcServer, server)
	server.Logging.Infof("started server at:%v", listener.Addr())
	grpcServer.Serve(listener)
	select {}
}
