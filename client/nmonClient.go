//Package client - includes all required monitoring objects for the client
package client

import (
	"sync"
	"time"

	proto "github.com/denizaydin/nmon/api"
	"github.com/sirupsen/logrus"
)

//MonObject each monitoring object has id, timestamp that shows last configuration active time and a notify channel.We will be watching the last configuration updatatime and will kill those object that is not active shorrter than the servers received broadcast time
type MonObject struct {
	ConfigurationUpdatetime int64
	ThreadupdateTime        int64
	Object                  *proto.MonitoringObject
	Notify                  chan string
}

//NmonClient - Holds client parameters
type NmonClient struct {
	ConfigClient            *proto.Client
	IsConfigClientConnected bool
	StatsClient             *proto.Client
	StatsConnClient         proto.StatsClient
	IsStatsClientConnected  bool
	Statschannel            chan *proto.StatsObject
	MonObecjts              map[string]*MonObject
	MonObjectScanTimer      *time.Ticker
	Logging                 *logrus.Logger
	WaitChannel             chan int
	WaitGroup               *sync.WaitGroup
}

//runningPingObjects - Total number of ping monitoring objects
var runningPingObjects map[string]*MonObject

//runningResolveObjects - Total number of dns resolve monitoring objects
var runningResolveObjects map[string]*MonObject

//runningTraceObjects - Total number of traceroute monitoring objects
var runningTraceObjects map[string]*MonObject

//Run - This is blocking function that will create, delete or update monitoring objects of the client.
func (client *NmonClient) Run() {
	client.Logging.Trace("nmonClient: runninf monitoring objects checks which will create, update or delete them")
	runningPingObjects := make(map[string]*MonObject)
	runningResolveObjects := make(map[string]*MonObject)
	runningTraceObjects := make(map[string]*MonObject)
	for {
		client.Logging.Debugf("nmonClient: current running number of monitor objects, ping:%v, resolve:%v, trace:%v", len(runningPingObjects), len(runningResolveObjects), len(runningTraceObjects))
		for key, monObject := range client.MonObecjts {
			switch t := monObject.Object.Object.(type) {
			case *proto.MonitoringObject_Pingdest:
				client.Logging.Tracef("nmonClient: checking ping object:%T", t)
				if _, ok := runningPingObjects[key]; ok {
					client.Logging.Tracef("nmonClient: pinger:%v current time:%v, configuration updatetime:%v, threadupdatime:%v, timeout:%v and interval:%v", key, time.Now().UnixNano(), monObject.ConfigurationUpdatetime, int64(runningPingObjects[key].ThreadupdateTime), int64(monObject.Object.GetPingdest().Timeout)*1000000000, int64(runningPingObjects[key].Object.GetPingdest().Interval)*2000000)
					if (time.Now().UnixNano() - monObject.ConfigurationUpdatetime) > (int64(monObject.Object.GetPingdest().Timeout) * 1000000000) {
						client.Logging.Tracef("nmonClient: found timeout ping:%v object updated:%v , stopping it", key, (time.Now().UnixNano() - monObject.ConfigurationUpdatetime))
						runningPingObjects[key].Notify <- "done"
						client.Logging.Tracef("nmonClient: stopped ping:%v object, removing from running ping object list", key)
						delete(runningPingObjects, key)
						client.Logging.Trace("nmonClient: removing ping:%v object from the ping object list", key)
						delete(client.MonObecjts, key)
						client.Logging.Debugf("nmonClient: deleted ping:%v object", key)
					} else if time.Now().UnixNano()-runningPingObjects[key].ThreadupdateTime > (int64(runningPingObjects[key].Object.GetPingdest().Interval) * 2000000) {
						client.Logging.Infof("nmonClient: found dead ping:%v object updated:%v, respawning", key, time.Now().UnixNano()-runningPingObjects[key].ThreadupdateTime)
						/*runningPingObjects[key] = &MonObject{
							ConfigurationUpdatetime: time.Now().UnixNano(),
							Object:                  monObject.Object,
							Notify:                  make(chan string),
						}
						go CheckPingDestination(runningPingObjects[key], client)*/
					} else {
						client.Logging.Debugf("nmonClient: updating ping object:%v, values:", key, monObject.Object.GetPingdest())
						runningPingObjects[key].Object.GetPingdest().Interval = monObject.Object.GetPingdest().GetInterval()
						runningPingObjects[key].Object.GetPingdest().Timeout = monObject.Object.GetPingdest().GetTimeout()
						runningPingObjects[key].Object.GetPingdest().PacketSize = monObject.Object.GetPingdest().GetPacketSize()
						runningPingObjects[key].Object.GetPingdest().Ttl = monObject.Object.GetPingdest().GetTtl()
					}
				} else {
					runningPingObjects[key] = &MonObject{
						ConfigurationUpdatetime: time.Now().UnixNano(),
						Object:                  monObject.Object,
						Notify:                  make(chan string),
					}
					client.Logging.Infof("nmonClient: creating ping object:%v, values:", key, monObject.Object.GetPingdest())
					go CheckPingDestination(runningPingObjects[key], client)
				}
			case *proto.MonitoringObject_Resolvedest:
				client.Logging.Debugf("nmonClient: checking resolve object:%T", t)
				if _, ok := runningResolveObjects[key]; ok {
					client.Logging.Tracef("nmonClient: found resolve:%v object and its last updated %v ago", key, time.Unix(0, monObject.ConfigurationUpdatetime))
					client.Logging.Tracef("nmonClient: resolver:%v current time:%v, configuration updatetime:%v, threadupdatime:%v, timeout:%v and interval:%v", key, time.Now().UnixNano(), monObject.ConfigurationUpdatetime, int64(runningResolveObjects[key].ThreadupdateTime), int64(monObject.Object.GetResolvedest().Timeout)*1000000000, int64(runningResolveObjects[key].Object.GetResolvedest().Interval)*2000000)
					if (time.Now().UnixNano() - monObject.ConfigurationUpdatetime) > (int64(monObject.Object.GetResolvedest().Timeout) * 1000000000) {
						client.Logging.Infof("nmonClient: found timeout resolve:%v object updated:%v, stopping it", key, time.Now().UnixNano()-monObject.ConfigurationUpdatetime)
						runningResolveObjects[key].Notify <- "done"
						client.Logging.Tracef("nmonClient: stopped resolve:%v object, removing from running resolve object list", key)
						delete(runningResolveObjects, key)
						client.Logging.Trace("nmonClient: removing resolve:%v object, from the resolve object list", key)
						delete(client.MonObecjts, key)
						client.Logging.Debugf("nmonClient: deleted resolve:%v object", key)
					} else if time.Now().UnixNano()-runningResolveObjects[key].ThreadupdateTime > (int64(runningResolveObjects[key].Object.GetResolvedest().Interval) * 2000000) {
						client.Logging.Infof("nmonClient: found dead resolve:%v object updated:%v, respawning", key, time.Now().UnixNano()-runningResolveObjects[key].ThreadupdateTime)
						/*runningResolveObjects[key] = &MonObject{
							ConfigurationUpdatetime: time.Now().UnixNano(),
							Object:                  monObject.Object,
							Notify:                  make(chan string),
						}
						client.Logging.Infof("nmonClient: created resolve monitoring object:%v", runningResolveObjects[key])
						go CheckResolveDestination(runningResolveObjects[key], client)*/
					} else {
						client.Logging.Debugf("nmonClient: updating resolve:%v object", key)
						runningResolveObjects[key].Object.GetResolvedest().Interval = monObject.Object.GetResolvedest().GetInterval()
						runningResolveObjects[key].Object.GetResolvedest().Timeout = monObject.Object.GetResolvedest().GetTimeout()
						runningResolveObjects[key].Object.GetResolvedest().ResolveServer = monObject.Object.GetResolvedest().GetResolveServer()
					}
				} else {
					runningResolveObjects[key] = &MonObject{
						ConfigurationUpdatetime: time.Now().UnixNano(),
						Object:                  monObject.Object,
						Notify:                  make(chan string),
					}
					client.Logging.Infof("nmonClient: created resolve monitoring object:%v", runningResolveObjects[key])
					go CheckResolveDestination(runningResolveObjects[key], client)
				}
			case *proto.MonitoringObject_Tracedest:
				client.Logging.Debugf("nmonClient: checking resolve object:%T", t)
				if _, ok := runningTraceObjects[key]; ok {
					client.Logging.Tracef("nmonClient: found trace:%v monitoring object and its last updated %v ago", key, time.Unix(0, monObject.ConfigurationUpdatetime))
					client.Logging.Tracef("nmonClient: tracer:%v current time:%v, configuration updatetime:%v, threadupdatime:%v, timeout:%v and interval:%v", key, time.Now().UnixNano(), monObject.ConfigurationUpdatetime, int64(runningTraceObjects[key].ThreadupdateTime), int64(monObject.Object.GetTracedest().Timeout)*1000000000, int64(runningTraceObjects[key].Object.GetTracedest().Interval)*1500000)
					if (time.Now().UnixNano() - monObject.ConfigurationUpdatetime) > (int64(monObject.Object.GetTracedest().Timeout) * 1000000000) {
						client.Logging.Infof("nmonClient: found timeout trace:%v object, stopping it", key)
						runningTraceObjects[key].Notify <- "done"
						client.Logging.Tracef("nmonClient: stopped trace:%v object, removing from running trace object list", key)
						delete(runningTraceObjects, key)
						client.Logging.Trace("nmonClient: removing trace:%v object, from the trace object list", key)
						delete(client.MonObecjts, key)
						client.Logging.Debugf("nmonClient: deleted trace:%v object", key)
					} else if time.Now().UnixNano()-runningTraceObjects[key].ThreadupdateTime > (int64(runningTraceObjects[key].Object.GetTracedest().Interval) * 1500000) {
						client.Logging.Infof("nmonClient: found dead trace:%v object, respawning", key)
						runningTraceObjects[key] = &MonObject{
							ConfigurationUpdatetime: time.Now().UnixNano(),
							Object:                  monObject.Object,
							Notify:                  make(chan string),
						}
						go CheckTraceDestination(runningTraceObjects[key], client)
					} else {
						client.Logging.Debugf("nmonClient: updating running trace object:%v", key)
						runningTraceObjects[key].Object.GetTracedest().Interval = monObject.Object.GetTracedest().GetInterval()
						runningTraceObjects[key].Object.GetTracedest().Timeout = monObject.Object.GetTracedest().GetTimeout()
					}
				} else {
					runningTraceObjects[key] = &MonObject{
						ConfigurationUpdatetime: time.Now().UnixNano(),
						Object:                  monObject.Object,
						Notify:                  make(chan string),
					}
					client.Logging.Infof("nmonClient: created trace:%v object", runningTraceObjects[key])
					go CheckTraceDestination(runningTraceObjects[key], client)
				}
			default:
				client.Logging.Warnf("nmonClient: unimplemented monitoring object type:%T", t)
			}
		}
		client.Logging.Trace("nmonClient: sleeping for 3sec for scanning monobjects")
		stat := &proto.StatsObject{
			Client:    client.StatsClient,
			Timestamp: time.Now().UnixNano(),
			Object: &proto.StatsObject_Clientstat{
				Clientstat: &proto.ClientStat{
					NumberOfMonObjects: int32(len(runningPingObjects) + len(runningTraceObjects) + len(runningResolveObjects)),
				},
			},
		}
		client.Statschannel <- stat
		client.Logging.Tracef("nmonClient: sent client stat:%v", stat)
		time.Sleep(1 * time.Second)
	}
}

//Stop - Stops all running monitor objects
func (client *NmonClient) Stop() {
	client.Logging.Debug("nmonClient: stopping all monitoring objects")
	for key := range runningPingObjects {
		client.Logging.Tracef("nmonClient: tring to stop ping:%v object", key)
		runningPingObjects[key].Notify <- "done"
		client.Logging.Tracef("nmonClient: stopped ping:%v object", key)
	}
	for key := range runningResolveObjects {
		client.Logging.Tracef("nmonClient: tring to stop resolve:%v object", key)
		runningResolveObjects[key].Notify <- "done"
		client.Logging.Tracef("nmonClient: stopped resolve:%v object", key)
	}
	for key := range runningTraceObjects {
		client.Logging.Tracef("nmonClient: tring to stop trace:%v object", key)
		runningTraceObjects[key].Notify <- "done"
		client.Logging.Tracef("nmonClient: stopped trace:%v object", key)
	}
	client.Logging.Debug("nmonClient: stopped all monitoring objects")
}
