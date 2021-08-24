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
	"encoding/binary"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	proto "github.com/denizaydin/nmon/api"
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

//Stat - Holds varibles of the received stat
type Stat struct {
	ClientName    string
	ClientAS      float64
	CientASHolder string
	NmonStat      *proto.StatsObject
	//StatsGRPCServerAddr - holds grpc server net address
}

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
	IpHolder      map[string]string
	PromCollector *promStatsCollector
	//stats channel to promeheous collecter
	PromCollectorChannel chan *Stat
}

type promStatsCollector struct {
	clientMetric *prometheus.Desc
	pingMetric   *prometheus.Desc
	resolveRTT   *prometheus.Desc
	resolvedIP   *prometheus.Desc
	traceHopRTT  *prometheus.Desc
	traceHopTTL  *prometheus.Desc
}

func newPromStatsCollector() *promStatsCollector {
	return &promStatsCollector{
		clientMetric: prometheus.NewDesc("clientMetric",
			"Number of monitoring objects count of the client",
			[]string{"clientname", "clientas"}, nil,
		),
		pingMetric: prometheus.NewDesc("pingMetric",
			"Ping RTT for the destination in msec",
			[]string{"clientname", "clientas", "clientholder", "destination"}, nil,
		),
		resolveRTT: prometheus.NewDesc("resolveRTT",
			"DNS Resolve RTT for the destination in msec",
			[]string{"clientname", "clientas", "clientholder", "destination", "resolver"}, nil,
		),
		resolvedIP: prometheus.NewDesc("resolvedIP",
			"Resolved IP address for the destination in float",
			[]string{"clientname", "clientas", "clientholder", "destination", "resolver"}, nil,
		),
		traceHopRTT: prometheus.NewDesc("traceHopRTT",
			"RTT for each hop during trace to destination",
			[]string{"clientname", "clientas", "clientholder", "destination", "hopas", "hopholder", "hopip"}, nil,
		),
		traceHopTTL: prometheus.NewDesc("traceHopTTL",
			"TTL for each hop during trace to destination",
			[]string{"clientname", "clientas", "clientholder", "destination", "hopas", "hopholder", "hopip"}, nil,
		),
	}
}

func (collector *promStatsCollector) Describe(ch chan<- *prometheus.Desc) {

	//Update this section with the each metric you create for a given collector
	ch <- collector.clientMetric
	ch <- collector.pingMetric
	ch <- collector.resolveRTT
	ch <- collector.resolvedIP
	ch <- collector.traceHopRTT
	ch <- collector.traceHopTTL

}

func Ip2long(ipstr string) float64 {
	ip := net.ParseIP(ipstr)
	if ip == nil {
		return 0
	}
	ip = ip.To4()
	return float64(binary.BigEndian.Uint32(ip))
}

func (collector *promStatsCollector) Collect(ch chan<- prometheus.Metric) {

	stat, _ := <-statsserver.PromCollectorChannel
	switch t := stat.NmonStat.Object.(type) {
	//should we use our timestamp, time.now instead of the client timestamp?
	case *proto.StatsObject_Clientstat:
		//ch <- *
		statsserver.Logging.Tracef("statsserver: prom collector received client stat:%v", stat)
		ps := prometheus.NewMetricWithTimestamp(time.Now(), prometheus.MustNewConstMetric(statsserver.PromCollector.clientMetric, prometheus.GaugeValue, float64(stat.NmonStat.GetClientstat().GetNumberOfMonObjects()), stat.ClientName, fmt.Sprintf("%f", stat.ClientAS)))
		ch <- ps
		statsserver.Logging.Debugf("statsserver: prom collector succecssfully write client stat:%v", ps)
	case *proto.StatsObject_Pingstat:
		statsserver.Logging.Tracef("statsserver: prom collector received ping stat:%v", stat)
		ps := prometheus.NewMetricWithTimestamp(time.Now(), prometheus.MustNewConstMetric(statsserver.PromCollector.pingMetric, prometheus.GaugeValue, float64(stat.NmonStat.GetPingstat().GetRtt()), stat.ClientName, fmt.Sprintf("%f", stat.ClientAS), stat.CientASHolder, stat.NmonStat.GetPingstat().GetDestination()))
		ch <- ps
		statsserver.Logging.Debugf("statsserver: prom collector succecssfully write ping stat:%v", ps)
	case *proto.StatsObject_Resolvestat:
		statsserver.Logging.Tracef("statsserver: prom collector received resolve stat:%v", stat)
		ps := prometheus.NewMetricWithTimestamp(time.Now(), prometheus.MustNewConstMetric(statsserver.PromCollector.resolveRTT, prometheus.GaugeValue, float64(stat.NmonStat.GetResolvestat().GetRtt()), stat.ClientName, fmt.Sprintf("%f", stat.ClientAS), stat.CientASHolder, stat.NmonStat.GetResolvestat().GetDestination(), stat.NmonStat.GetResolvestat().Resolver))
		ch <- ps
		statsserver.Logging.Debugf("statsserver: prom collector succecssfully write resolve rtt stat:%v", ps)
		ps = prometheus.NewMetricWithTimestamp(time.Now(), prometheus.MustNewConstMetric(statsserver.PromCollector.resolvedIP, prometheus.GaugeValue, Ip2long(stat.NmonStat.GetResolvestat().GetResolvedip()), stat.ClientName, fmt.Sprintf("%f", stat.ClientAS), stat.CientASHolder, stat.NmonStat.GetResolvestat().GetDestination(), stat.NmonStat.GetResolvestat().Resolver))
		ch <- ps
		statsserver.Logging.Debugf("statsserver: prom collector succecssfully write resolve resolved IP stat:%v", ps)
	case *proto.StatsObject_Tracestat:
		statsserver.Logging.Tracef("statsserver: prom collector received trace stat:%v", stat)
		ip := stat.NmonStat.GetTracestat().GetHopIP()
		_, ok := statsserver.IpASN[ip]
		if net.ParseIP(ip).IsGlobalUnicast() {

			if !ok {
				statsserver.IpASN[ip] = 0
				statsserver.IpHolder[ip] = "unknown"
				var p ripe.Prefix
				p.Set(ip)
				p.GetData()
				statsserver.Logging.Debugf("statsserver: got the tracehop information from ripe:%v", p)
				data, _ := p.Data["data"].(map[string]interface{})
				asns := data["asns"].([]interface{})
				//TODO: can it more than one ASN per prefix?
				for _, h := range asns {
					statsserver.IpHolder[ip] = h.(map[string]interface{})["holder"].(string)
					statsserver.IpASN[ip] = h.(map[string]interface{})["asn"].(float64)
				}
			}
		} else {
			statsserver.IpHolder[ip] = "private"
			statsserver.IpASN[ip] = 0
		}

		ps := prometheus.NewMetricWithTimestamp(time.Now(), prometheus.MustNewConstMetric(statsserver.PromCollector.traceHopRTT, prometheus.GaugeValue, float64(stat.NmonStat.GetTracestat().HopRTT), stat.ClientName, fmt.Sprintf("%f", stat.ClientAS), stat.CientASHolder, stat.NmonStat.GetTracestat().GetDestination(), fmt.Sprintf("%f", statsserver.IpASN[stat.NmonStat.GetTracestat().GetHopIP()]), statsserver.IpHolder[stat.NmonStat.GetTracestat().GetHopIP()], stat.NmonStat.GetTracestat().GetHopIP()))
		ch <- ps
		statsserver.Logging.Debugf("statsserver: prom collector succecssfully write trace hop rtt stat:%v", ps)

		ps = prometheus.NewMetricWithTimestamp(time.Now(), prometheus.MustNewConstMetric(statsserver.PromCollector.traceHopTTL, prometheus.GaugeValue, float64(stat.NmonStat.GetTracestat().HopTTL), stat.ClientName, fmt.Sprintf("%f", stat.ClientAS), stat.CientASHolder, stat.NmonStat.GetTracestat().GetDestination(), fmt.Sprintf("%f", statsserver.IpASN[stat.NmonStat.GetTracestat().GetHopIP()]), statsserver.IpHolder[stat.NmonStat.GetTracestat().GetHopIP()], stat.NmonStat.GetTracestat().GetHopIP()))
		ch <- ps
		statsserver.Logging.Debugf("statsserver: prom collector succecssfully write trace hop ttl stat:%v", ps)

	default:
		statsserver.Logging.Errorf("statsserver: unexpected statistic object type %T", t)
	}

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

	pingStatWithTSDesc = prometheus.NewDesc(
		"pingstats_withTimestamp",
		"ping rtt in miliseconds",
		nil, nil,
	)

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
	statsserver.Logging.SetOutput(os.Stdout)
	logLevel := "info"
	flag.StringVar(&logLevel, "loglevel", "disable", "disable,info, error, warning,debug or trace")
	flag.StringVar(&statsserver.StatsGRPCServerAddr, "grpcaddr", "localhost:8081", "GRPC server Net Address")
	flag.StringVar(&statsserver.PromMetricServerAddr, "metricaddr", "localhost:8082", "Prometheus Metric Net Address")
	flag.Parse()

	switch logLevel {
	case "disable":
		statsserver.Logging.SetOutput(ioutil.Discard)
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

	statsserver.PromCollector = newPromStatsCollector()
	prometheus.MustRegister(statsserver.PromCollector)
	statsserver.IpASN = make(map[string]float64)
	statsserver.IpHolder = make(map[string]string)
	statsserver.PromCollectorChannel = make(chan *Stat)
	statsserver.Logging.Info("running nmon stats service")
}

//RecordStats - Client streaming process, clients are send stats with this method.
//Recives client statistic
func (s *StatsServer) RecordStats(stream proto.Stats_RecordStatsServer) error {
	pr, ok := peer.FromContext(stream.Context())
	if !ok {
		s.Logging.Warningf("statsserver: can not get client ip address:%v", pr.Addr)
	}
	s.Logging.Infof("statsserver: new client stats from ip:%v", pr.Addr)
	// TODO: for testing purposes from local computer,
	_, ipnet, neterr := net.ParseCIDR(pr.Addr.String() + "/24")
	ip := pr.Addr.String()
	if neterr != nil {
		ip = ipnet.String()
		s.Logging.Debugf("statsserver: changed client as storage info to:%v", ip)
	}
	if net.ParseIP(ip).IsGlobalUnicast() {
		s.Logging.Debugf("statsserver: retring client:%v info from ripe", pr.Addr)
		_, ok = s.IpASN[ip]
		if !ok {
			s.IpASN[ip] = 0
			s.IpHolder[ip] = "unknown"
			var p ripe.Prefix
			p.Set(ip)
			p.GetData()
			s.Logging.Debugf("statsserver: got the client information from ripe:%v", p)
			data, _ := p.Data["data"].(map[string]interface{})
			asns := data["asns"].([]interface{})
			//TODO: can it more than one ASN per prefix?
			for _, h := range asns {
				s.IpHolder[ip] = h.(map[string]interface{})["holder"].(string)
				s.IpASN[ip] = h.(map[string]interface{})["asn"].(float64)
			}
		}
	} else {
		s.Logging.Debugf("statsserver: setting client:%v info from to private", pr.Addr)
		s.IpHolder[ip] = "private"
		s.IpASN[ip] = 0
	}
	var err error
	for {
		receivedstat, err := stream.Recv()
		if err == nil {
			var stat = &Stat{
				ClientName:    receivedstat.GetClient().GetName(),
				ClientAS:      s.IpASN[ip],
				CientASHolder: s.IpHolder[ip],
				NmonStat:      &proto.StatsObject{},
			}
			s.Logging.Debugf("statsserver: received receivedstat:%v from the client:%v %v, relaying it to prometheus collector", receivedstat, receivedstat.GetClient().GetName(), receivedstat.GetClient().GetId())
			stat.NmonStat = receivedstat
			statsserver.PromCollectorChannel <- stat
		} else {
			s.Logging.Errorf("statsserver: during reading client receivedstat request from:%v", err)
			break
		}
	}
	return err
}

func main() {
	statsserver.Logging.Infof("statsserver: server is initialized with parameters:%v", statsserver)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs)
	go func() {
		s := <-sigs
		switch s {
		case syscall.SIGURG:
			statsserver.Logging.Infof("statsserver: received unhandled %v signal from os:", s)
		default:
			statsserver.Logging.Infof("statsserver: received %v signal from os,exiting", s)
			os.Exit(1)
		}
	}()

	go func() {
		statsserver.Logging.Infof("statsserver: prometheus server is initialized with parameters:%+v", statsserver.PromMetricServerAddr)
		reg := prometheus.NewPedanticRegistry()

		gatherers := prometheus.Gatherers{
			prometheus.DefaultGatherer,
			reg,
		}
		h := promhttp.HandlerFor(gatherers,
			promhttp.HandlerOpts{
				ErrorLog:      statsserver.Logging,
				ErrorHandling: promhttp.ContinueOnError,
			})
		http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r)
		})
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
	statsserver.Logging.Debugf("statsserver: starting server at:%v", statsserver.StatsGRPCServerAddr)
	listener, err := net.Listen("tcp", statsserver.StatsGRPCServerAddr)
	if err != nil {
		statsserver.Logging.Fatalf("statsserver: error creating the server %v", err)
	}
	proto.RegisterStatsServer(grpcServer, statsserver)
	statsserver.Logging.Infof("statsserver: started server at:%v", listener.Addr())
	grpcServer.Serve(listener)
	select {}
}
