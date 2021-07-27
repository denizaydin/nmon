package main

/* ==> Caveats
When run from `brew services`, `prometheus` is run from
`prometheus_brew_services` and uses the flags in:
   /usr/local/etc/prometheus.args

To have launchd start prometheus now and restart at login:
  brew services start prometheus
Or, if you don't want/need a background service you can just run:
  prometheus --config.file=/usr/local/etc/prometheus.yml
==> Summary
🍺  /usr/local/Cellar/prometheus/2.28.1: 21 files, 177.2MB */
import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	proto "dnzydn.com/nmon/api"
	ripe "github.com/mehrdadrad/mylg/ripe"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	logrus "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
)

var (
	//server - holds the statsserver variables
	statsserver *StatsServer
)

//Server - Holds varibles of the server
type StatsServer struct {
	Logging *logrus.Logger
	//StatsGRPCServerAddr - holds grpc server net address
	StatsGRPCServerAddr string
	//PromMetricServerAddr - holds prometheous metrics server net address
	PromMetricServerAddr string
	//IpASN - holds ip to asn mapping for caching
	IpASN map[string]float64
	//Server.IpHolder - holds ip to owner, holder mapping for caching
	IpHolder map[string]string
}

//Prometheus metric variables
var (
	//clientStat - client type of gauge metrics with labels
	clientStat = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "NMON",
		Subsystem: "clientstats",
		Name:      "totalMonitoringObjects",
		Help:      "client statistics",
	},
		[]string{
			// Which user has requested the operation?
			"clientName",
			"clientIP",
			"clientAS",
			"clientISP",
		})
	//pingStat - ping type of gauge metrics with labels
	pingStat = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "NMON",
		Subsystem: "pingstats",
		Name:      "pingStat",
		Help:      "ping statistics",
	},
		[]string{
			// Which user has requested the operation?
			"clientName",
			"clientIP",
			"clientAS",
			"clientISP",
			"destination",
		})
	//resolveStat - resolve type of gauge metrics with labels
	resolveStat = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "NMON",
		Subsystem: "resolvestats",
		Name:      "resolveStat",
		Help:      "resolve statistics",
	},
		[]string{
			// Which user has requested the operation?
			"clientName",
			"clientIP",
			"clientAS",
			"clientISP",
			"destination",
			"resolvedip",
			"resolver",
		})
	//traceStat - resolve type of gauge metrics with labels
	traceStat = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "NMON",
		Subsystem: "tracestat",
		Name:      "hopStat",
		Help:      "hop statistic during traceroute to destinations",
	},
		[]string{
			// Which user has requested the operation?
			"clientName",
			"clientIP",
			"clientAS",
			"clientISP",
			"destination",
			"hopIP",
			"hopTTL",
			"hopASN",
			"hopHolder",
		})
)

func init() {
	//Create new server instance
	statsserver = &StatsServer{
		Logging:              &logrus.Logger{},
		StatsGRPCServerAddr:  "",
		PromMetricServerAddr: "",
		IpASN:                map[string]float64{},
		IpHolder:             map[string]string{},
	}
	// Log as JSON instead of the default ASCII formatter.
	statsserver.Logging = logrus.New()
	statsserver.Logging.SetFormatter(&logrus.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})
	// Output to stdout instead of the default stderr
	// Can be any io.Writer, see below for File example
	statsserver.Logging.SetOutput(os.Stdout)
	logLevel := "info"
	flag.StringVar(&logLevel, "loglevel", "info", "info, error, warning,debug or trace")
	flag.StringVar(&statsserver.StatsGRPCServerAddr, "grpcaddr", "localhost:8081", "GRPC server Net Address")
	flag.StringVar(&statsserver.PromMetricServerAddr, "metricaddr", "localhost:8082", "Prometheus Metric Net Address")

	flag.Parse()
	switch logLevel {
	case "info":
		statsserver.Logging.SetLevel(logrus.InfoLevel)
	case "error":
		statsserver.Logging.SetLevel(logrus.ErrorLevel)
	case "warn":
		statsserver.Logging.SetLevel(logrus.WarnLevel)
	case "debug":
		statsserver.Logging.SetLevel(logrus.DebugLevel)
	case "trace":
		statsserver.Logging.SetLevel(logrus.TraceLevel)
	default:
		statsserver.Logging.SetLevel(logrus.DebugLevel)
	}
	prometheus.MustRegister(clientStat)
	prometheus.MustRegister(pingStat)
	prometheus.MustRegister(resolveStat)
	prometheus.MustRegister(traceStat)
	statsserver.IpASN = make(map[string]float64)
	statsserver.IpHolder = make(map[string]string)
	statsserver.Logging.Info("running nmon stats service")

}

//RecordStats - Client streaming process, clients are send stats with this method.
//Recives client statistic
func (s *StatsServer) RecordStats(stream proto.Stats_RecordStatsServer) error {
	pr, ok := peer.FromContext(stream.Context())
	if !ok {
		s.Logging.Warningf("can not get client ip address:%v", pr.Addr)
	}
	s.Logging.Infof("new client stats from ip:%v", pr.Addr)
	// TODO: for testing purposes from local computer,
	// ip := pr.Addr
	ip := "88.225.253.200"

	_, ok = s.IpASN[ip]
	if !ok {
		var p ripe.Prefix
		p.Set(ip)
		p.GetData()
		s.Logging.Debugf("got the client information from ripe:%v", p)
		data, _ := p.Data["data"].(map[string]interface{})
		asns := data["asns"].([]interface{})
		//TODO: can it more than one ASN per prefix?
		for _, h := range asns {
			s.IpHolder[ip] = h.(map[string]interface{})["holder"].(string)
			s.IpASN[ip] = h.(map[string]interface{})["asn"].(float64)
		}
	}

	for {
		stats, err := stream.Recv()
		if err != nil {
			s.Logging.Errorf("during reading client stats request from:%v", err)
			return err
		}
		s.Logging.Debugf("received stats:%v from the client:%v %v", stats, stats.GetClient().GetName(), stats.GetClient().GetId())
		switch t := stats.Object.(type) {
		case *proto.StatsObject_Clientstat:
			s.Logging.Tracef("client stats, number of monitoring objects is:%v", stats.GetClientstat().GetNumberOfMonObjects())
			clientStat.WithLabelValues(stats.GetClient().GetName(), ip, fmt.Sprint(s.IpASN[ip]), s.IpHolder[ip]).Set(float64(stats.GetClientstat().GetNumberOfMonObjects()))
		case *proto.StatsObject_Pingstat:
			s.Logging.Tracef("ping stats for destination:%v, rtt:%v", stats.GetPingstat().GetDestination(), stats.GetPingstat().GetRtt())
			pingStat.WithLabelValues(stats.GetClient().GetName(), ip, fmt.Sprint(s.IpASN[ip]), s.IpHolder[ip], stats.GetPingstat().GetDestination()).Set(float64(stats.GetPingstat().GetRtt()))
		case *proto.StatsObject_Resolvestat:
			s.Logging.Tracef("resolve stats for destination:%v, rtt:%v", stats.GetResolvestat().GetDestination(), stats.GetResolvestat().GetRtt())
			resolveStat.WithLabelValues(stats.GetClient().GetName(), ip, fmt.Sprint(s.IpASN[ip]), s.IpHolder[ip], stats.GetResolvestat().GetDestination(), stats.GetResolvestat().GetResolvedip(), stats.GetResolvestat().GetResolver()).Set(float64(stats.GetResolvestat().GetRtt()))
		case *proto.StatsObject_Tracestat:
			s.Logging.Tracef("trace stats for destination:%v, hop:%v, ttl:%v, rtt:%v", stats.GetTracestat().GetDestination(), stats.GetTracestat().GetHopIP(), fmt.Sprint(stats.GetTracestat().GetHopTTL()), float64(stats.GetTracestat().GetHopRTT()))
			_, ok = s.IpASN[stats.GetTracestat().GetHopIP()]
			if !ok {
				var p ripe.Prefix
				p.Set(ip)
				p.GetData()
				s.Logging.Debugf("hop information from ripe:%v", p)
				data, _ := p.Data["data"].(map[string]interface{})
				asns := data["asns"].([]interface{})
				for _, h := range asns {
					s.IpHolder[stats.GetTracestat().GetHopIP()] = h.(map[string]interface{})["holder"].(string)
					s.IpASN[stats.GetTracestat().GetHopIP()] = h.(map[string]interface{})["asn"].(float64)
				}
			}
			traceStat.WithLabelValues(
				stats.GetClient().GetName(), ip, fmt.Sprint(s.IpASN[ip]), s.IpHolder[ip],
				stats.GetTracestat().GetDestination(),
				stats.GetTracestat().GetHopIP(), fmt.Sprint(stats.GetTracestat().GetHopTTL()), fmt.Sprint(s.IpASN[stats.GetTracestat().GetHopIP()]), s.IpHolder[stats.GetTracestat().GetHopIP()]).Set(float64(stats.GetTracestat().GetHopRTT()))
		case nil:
			// The field is not set.
		default:
			s.Logging.Errorf("unexpected statistic object type %T", t)
		}

	}
}

func main() {
	statsserver.Logging.Infof("server is initialized with parameters:%v", statsserver)
	go func() {
		statsserver.Logging.Infof("prometheus server is initialized with parameters:%+v", statsserver.PromMetricServerAddr)
		http.Handle("/metrics", promhttp.Handler())
		panic(http.ListenAndServe(statsserver.PromMetricServerAddr, nil))
		prometheus.DefMaxAge.Hours()
	}()
	grpcServer := grpc.NewServer(grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
		MinTime:             5 * time.Second, // If a client pings more than once every 5 seconds, terminate the connection
		PermitWithoutStream: true,            // Allow pings even when there are no active streams
	}), grpc.KeepaliveParams(keepalive.ServerParameters{
		MaxConnectionIdle: 15 * time.Second, // If a client is idle for 15 seconds, send a GOAWAY
		Time:              5 * time.Second,  // Ping the client if it is idle for 5 seconds to ensure the connection is still active
		Timeout:           1 * time.Second,  // Wait 1 second for the ping ack before assuming the connection is dead
	}))
	statsserver.Logging.Debugf("starting server at:%v", statsserver.StatsGRPCServerAddr)
	listener, err := net.Listen("tcp", statsserver.StatsGRPCServerAddr)
	if err != nil {
		statsserver.Logging.Fatalf("error creating the server %v", err)
	}
	proto.RegisterStatsServer(grpcServer, statsserver)
	statsserver.Logging.Infof("started server at:%v", listener.Addr())
	grpcServer.Serve(listener)
	select {}
}
