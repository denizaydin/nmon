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
	"strings"
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
	ClientName     string
	ClientAS       string
	ClientASHolder string
	ClientNet      string
	NmonStat       *proto.StatsObject
	//StatsGRPCServerAddr - holds grpc server net address
}

//Server - Holds varibles of the server
type StatsServer struct {
	Logging *logrus.Logger
	//StatsGRPCServerAddr - holds grpc server net address
	StatsGRPCServerAddr string
	//PromMetricServerAddr - holds prometheous metrics server net address
	PromMetricServerAddr string
	//IPASN - holds ip to asn mapping for caching
	IPASN map[string]float64
	//Server.IPHOLDER - holds ip to owner, holder mapping for caching
	IPHOLDER      map[string]string
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
			[]string{"clientname", "clientas", "clientholder", "clientnet"}, nil,
		),
		pingMetric: prometheus.NewDesc("pingMetric",
			"Ping RTT for the destination in msec",
			[]string{"clientname", "clientas", "clientholder", "clientnet", "destination"}, nil,
		),
		resolveRTT: prometheus.NewDesc("resolveRTT",
			"DNS Resolve RTT for the destination in msec",
			[]string{"clientname", "clientas", "clientholder", "clientnet", "destination", "resolver"}, nil,
		),
		resolvedIP: prometheus.NewDesc("resolvedIP",
			"Resolved IP address for the destination in float",
			[]string{"clientname", "clientas", "clientholder", "clientnet", "destination", "resolver"}, nil,
		),
		traceHopRTT: prometheus.NewDesc("traceHopRTT",
			"RTT for each hop during trace to destination",
			[]string{"clientname", "clientas", "clientholder", "clientnet", "destination", "hopas", "hopholder", "hopip"}, nil,
		),
		traceHopTTL: prometheus.NewDesc("traceHopTTL",
			"TTL for each hop during trace to destination",
			[]string{"clientname", "clientas", "clientholder", "clientnet", "destination", "hopas", "hopholder", "hopip"}, nil,
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

func (collector *promStatsCollector) Collect(ch chan<- prometheus.Metric) {

	stat, _ := <-statsserver.PromCollectorChannel
	switch t := stat.NmonStat.Object.(type) {
	//should we use our timestamp, time.now instead of the client timestamp?
	case *proto.StatsObject_Clientstat:
		//ch <- *
		statsserver.Logging.Tracef("statsserver: prom collector received client stat:%v", stat)
		ps := prometheus.NewMetricWithTimestamp(time.Now(), prometheus.MustNewConstMetric(statsserver.PromCollector.clientMetric, prometheus.GaugeValue, float64(stat.NmonStat.GetClientstat().GetNumberOfMonObjects()), stat.ClientName, fmt.Sprintf("%f", stat.ClientAS), stat.ClientASHolder, stat.ClientNet))
		ch <- ps
		statsserver.Logging.Debugf("statsserver: prom collector succecssfully write client stat:%v", ps)
	case *proto.StatsObject_Pingstat:
		statsserver.Logging.Tracef("statsserver: prom collector received ping stat:%v", stat)
		ps := prometheus.NewMetricWithTimestamp(time.Now(), prometheus.MustNewConstMetric(statsserver.PromCollector.pingMetric, prometheus.GaugeValue, float64(stat.NmonStat.GetPingstat().GetRtt()), stat.ClientName, fmt.Sprintf("%f", stat.ClientAS), stat.ClientASHolder, stat.ClientNet, stat.NmonStat.GetPingstat().GetDestination()))
		ch <- ps
		statsserver.Logging.Debugf("statsserver: prom collector succecssfully write ping stat:%v", ps)
	case *proto.StatsObject_Resolvestat:
		statsserver.Logging.Tracef("statsserver: prom collector received resolve stat:%v", stat)
		ps := prometheus.NewMetricWithTimestamp(time.Now(), prometheus.MustNewConstMetric(statsserver.PromCollector.resolveRTT, prometheus.GaugeValue, float64(stat.NmonStat.GetResolvestat().GetRtt()), stat.ClientName, fmt.Sprintf("%f", stat.ClientAS), stat.ClientASHolder, stat.ClientNet, stat.NmonStat.GetResolvestat().GetDestination(), stat.NmonStat.GetResolvestat().Resolver))
		ch <- ps
		statsserver.Logging.Debugf("statsserver: prom collector succecssfully write resolve rtt stat:%v", ps)
		ps = prometheus.NewMetricWithTimestamp(time.Now(), prometheus.MustNewConstMetric(statsserver.PromCollector.resolvedIP, prometheus.GaugeValue, IP2long(stat.NmonStat.GetResolvestat().GetResolvedip()), stat.ClientName, fmt.Sprintf("%f", stat.ClientAS), stat.ClientASHolder, stat.ClientNet, stat.NmonStat.GetResolvestat().GetDestination(), stat.NmonStat.GetResolvestat().Resolver))
		ch <- ps
		statsserver.Logging.Debugf("statsserver: prom collector succecssfully write resolve resolved IP stat:%v", ps)
	case *proto.StatsObject_Tracestat:
		statsserver.Logging.Tracef("statsserver: prom collector received trace stat:%v", stat)
		hopIP := stat.NmonStat.GetTracestat().GetHopIP()
		hopAs := "unknown"
		hopHolder := "unknown"
		hopNet := hopIP + "/24"
		_, ipnet, neterr := net.ParseCIDR(hopIP + "/24")
		if neterr == nil {
			hopNet = ipnet.String()
		}
		statsserver.Logging.Infof("statsserver: new hop stats for ip:%v net:%v", hopIP, hopNet)
		if net.ParseIP(hopIP).IsGlobalUnicast() && !net.ParseIP(hopIP).IsPrivate() {
			statsserver.Logging.Debugf("statsserver: retriving the hop:%v information from ripe", hopIP)
			_, ok := statsserver.IPASN[hopNet]
			if !ok {
				var p ripe.Prefix
				p.Set(hopNet)
				p.GetData()
				statsserver.Logging.Debugf("statsserver: got the hop information from ripe:%v", p)
				data, _ := p.Data["data"].(map[string]interface{})
				asns := data["asns"].([]interface{})
				//TODO: can it more than one ASN per prefix?
				for _, h := range asns {
					statsserver.IPHOLDER[hopNet] = h.(map[string]interface{})["holder"].(string)
					statsserver.IPASN[hopNet] = h.(map[string]interface{})["asn"].(float64)
				}
			}
			hopAs = fmt.Sprintf("%f", statsserver.IPHOLDER[hopNet])
			hopHolder = statsserver.IPHOLDER[hopNet]
		}

		ps := prometheus.NewMetricWithTimestamp(time.Now(), prometheus.MustNewConstMetric(statsserver.PromCollector.traceHopRTT, prometheus.GaugeValue, float64(stat.NmonStat.GetTracestat().HopRTT), stat.ClientName, stat.ClientNet, fmt.Sprintf("%f", stat.ClientAS), stat.ClientASHolder, stat.NmonStat.GetTracestat().GetDestination(), hopAs, hopHolder, stat.NmonStat.GetTracestat().GetHopIP()))
		ch <- ps
		statsserver.Logging.Debugf("statsserver: prom collector succecssfully write trace hop rtt stat:%v", ps)

		ps = prometheus.NewMetricWithTimestamp(time.Now(), prometheus.MustNewConstMetric(statsserver.PromCollector.traceHopTTL, prometheus.GaugeValue, float64(stat.NmonStat.GetTracestat().HopTTL), stat.ClientName, stat.ClientNet, fmt.Sprintf("%f", stat.ClientAS), stat.ClientASHolder, stat.NmonStat.GetTracestat().GetDestination(), hopAs, hopHolder, stat.NmonStat.GetTracestat().GetHopIP()))
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
		IPASN:                map[string]float64{},
		IPHOLDER:             map[string]string{},
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
	statsserver.IPASN = make(map[string]float64)
	statsserver.IPHOLDER = make(map[string]string)
	statsserver.PromCollectorChannel = make(chan *Stat, 100)
	statsserver.Logging.Info("running nmon stats service")
}

//IP2long - returng ip address as float64 for prometheus metric recording
func IP2long(ipstr string) float64 {
	ip := net.ParseIP(ipstr)
	if ip == nil {
		return 0
	}
	ip = ip.To4()
	return float64(binary.BigEndian.Uint32(ip))
}

//RecordStats - Client streaming process, clients are send stats with this method.
//Recives client statistic
func (s *StatsServer) RecordStats(stream proto.Stats_RecordStatsServer) error {
	pr, ok := peer.FromContext(stream.Context())
	//initial values
	clientIP := "127.0.0.1:1"
	clientAs := "unknown"
	clientHolder := "unknown"
	clientNet := clientIP + "/24"
	if ok {
		s.Logging.Tracef("statsserver: get client ip address:%v", pr.Addr)
		clientIP = strings.Split(pr.Addr.String(), ":")[0]
	}

	s.Logging.Debugf("statsserver: get statistic from the ip address:%v", clientIP)
	_, ipnet, neterr := net.ParseCIDR(clientIP + "/24")
	if neterr == nil {
		clientNet = ipnet.String()
	}
	s.Logging.Infof("statsserver: new client stats from ip:%v net:%v", clientIP, clientNet)
	if net.ParseIP(clientIP).IsGlobalUnicast() && !net.ParseIP(clientIP).IsPrivate() {
		s.Logging.Debugf("statsserver: retriving the client:%v information from ripe", clientIP)
		_, ok = s.IPASN[clientNet]
		if !ok {
			var p ripe.Prefix
			p.Set(clientNet)
			p.GetData()
			s.Logging.Debugf("statsserver: got the client information from ripe:%v", p)
			data, _ := p.Data["data"].(map[string]interface{})
			asns := data["asns"].([]interface{})
			//TODO: can it more than one ASN per prefix?
			for _, h := range asns {
				s.IPHOLDER[clientNet] = h.(map[string]interface{})["holder"].(string)
				s.IPASN[clientNet] = h.(map[string]interface{})["asn"].(float64)
			}
		}
		clientAs = fmt.Sprintf("%f", s.IPHOLDER[clientNet])
		clientHolder = s.IPHOLDER[clientNet]
	}

	var err error
	for {
		receivedstat, err := stream.Recv()
		if err == nil {
			var stat = &Stat{
				ClientName:     receivedstat.GetClient().GetName(),
				ClientAS:       clientAs,
				ClientASHolder: clientHolder,
				ClientNet:      clientNet,
				NmonStat:       receivedstat,
			}
			statsserver.PromCollectorChannel <- stat
			s.Logging.Debugf("statsserver: received stat:%v from the client:%v %v, relayed it to prometheus collector", receivedstat, receivedstat.GetClient().GetName(), receivedstat.GetClient().GetId())
		} else {
			s.Logging.Errorf("statsserver: during reading client received stat request from:%v", err)
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
}
