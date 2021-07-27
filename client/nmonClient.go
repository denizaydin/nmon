//Package client - includes all required monitoring objects for the client
package client

import (
	"sync"
	"time"

	proto "dnzydn.com/nmon/api"
	"github.com/sirupsen/logrus"
)

//MonObject each monitoring object has id, timestamp that shows last configuration active time and a notify channel.We will be watching the last configuration updatatime and will kill those object that is not active shorrter than the servers received broadcast time
type MonObject struct {
	ConfigurationUpdatetime int64
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
	client.Logging.Trace("runninf monitoring objects checks which will create, update or delete them")
	runningPingObjects := make(map[string]*MonObject)
	runningResolveObjects := make(map[string]*MonObject)
	runningTraceObjects := make(map[string]*MonObject)
	for {
		client.Logging.Debugf("current running number of monitor objects, ping:%v, resolve:%v, trace:%v", len(runningPingObjects), len(runningResolveObjects), len(runningTraceObjects))
		for key, monObject := range client.MonObecjts {
			switch t := monObject.Object.Object.(type) {
			case *proto.MonitoringObject_Pingdest:
				client.Logging.Tracef("checking ping object:%T", t)
				if _, ok := runningPingObjects[key]; ok {
					client.Logging.Tracef("found ping:%v object and its last updated %v ago", key, time.Unix(0, monObject.ConfigurationUpdatetime))
					if (time.Now().UnixNano() - monObject.ConfigurationUpdatetime) > (int64(monObject.Object.GetPingdest().Timeout) * 1000000000) {
						client.Logging.Tracef("current time:%v, configuration updatetime:%v and timeout:%v", time.Now().UnixNano(), monObject.ConfigurationUpdatetime, int64(monObject.Object.GetPingdest().Timeout)*1000000000)
						client.Logging.Tracef("found timeout ping:%v object, stopping it", key)
						runningPingObjects[key].Notify <- "done"
						client.Logging.Tracef("stopped ping:%v object, removing from running ping object list", key)
						delete(runningPingObjects, key)
						client.Logging.Trace("removing ping:%v object from the ping object list", key)
						delete(client.MonObecjts, key)
						client.Logging.Debugf("deleted ping:%v object", key)
					} else {
						client.Logging.Debugf("updating ping object:%v, values:", key, monObject.Object.GetPingdest())
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
					client.Logging.Infof("creating ping object:%v, values:", key, monObject.Object.GetPingdest())
					go CheckPingDestination(runningPingObjects[key], client)
				}
			case *proto.MonitoringObject_Resolvedest:
				client.Logging.Debugf("checking resolve object:%T", t)
				if _, ok := runningResolveObjects[key]; ok {
					client.Logging.Tracef("found resolve:%v object and its last updated %v ago", key, time.Unix(0, monObject.ConfigurationUpdatetime))
					if (time.Now().UnixNano() - monObject.ConfigurationUpdatetime) > (int64(monObject.Object.GetResolvedest().Timeout) * 1000000000) {
						client.Logging.Tracef("current time:%v, configuration updatetime:%v and timeout:%v", time.Now().UnixNano(), monObject.ConfigurationUpdatetime, int64(monObject.Object.GetResolvedest().Timeout)*1000000000)
						client.Logging.Infof("found timeout resolve:%v object, stopping it", key)
						runningResolveObjects[key].Notify <- "done"
						client.Logging.Tracef("stopped resolve:%v object, removing from running resolve object list", key)
						delete(runningResolveObjects, key)
						client.Logging.Trace("removing resolve:%v object, from the resolve object list", key)
						delete(client.MonObecjts, key)
						client.Logging.Debugf("deleted resolve:%v object", key)
					} else {
						client.Logging.Debugf("updating resolve:%v object", key)
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
					client.Logging.Infof("created resolve monitoring object:%v", runningResolveObjects[key])
					go CheckResolveDestination(runningResolveObjects[key], client)
				}
			case *proto.MonitoringObject_Tracedest:
				client.Logging.Debugf("checking resolve object:%T", t)
				if _, ok := runningTraceObjects[key]; ok {
					client.Logging.Tracef("found trace:%v monitoring object and its last updated %v ago", key, time.Unix(0, monObject.ConfigurationUpdatetime))
					if (time.Now().UnixNano() - monObject.ConfigurationUpdatetime) > (int64(monObject.Object.GetTracedest().Timeout) * 1000000000) {
						client.Logging.Tracef("current time:%v, configuration updatetime:%v and timeout:%v", time.Now().UnixNano(), monObject.ConfigurationUpdatetime, int64(monObject.Object.GetTracedest().Timeout)*1000000000)
						client.Logging.Infof("found timeout trace:%v object, stopping it", key)
						runningTraceObjects[key].Notify <- "done"
						client.Logging.Tracef("stopped trace:%v object, removing from running trace object list", key)
						delete(runningTraceObjects, key)
						client.Logging.Trace("removing trace:%v object, from the trace object list", key)
						delete(client.MonObecjts, key)
						client.Logging.Debugf("deleted trace:%v object", key)
					} else {
						client.Logging.Debugf("updating running trace object:%v", key)
						runningTraceObjects[key].Object.GetTracedest().Interval = monObject.Object.GetTracedest().GetInterval()
						runningTraceObjects[key].Object.GetTracedest().Timeout = monObject.Object.GetTracedest().GetTimeout()
					}
				} else {
					runningTraceObjects[key] = &MonObject{
						ConfigurationUpdatetime: time.Now().UnixNano(),
						Object:                  monObject.Object,
						Notify:                  make(chan string),
					}
					client.Logging.Infof("created trace:%v object", runningTraceObjects[key])
					go CheckTraceDestination(runningTraceObjects[key], client)
				}
			default:
				client.Logging.Warnf("unimplemented monitoring object type:%T", t)
			}
		}
		client.Logging.Trace("sleeping for 1 seconds")
		time.Sleep(1 * time.Second)
		client.Logging.Trace("waked from sleeping")
	}
}

//Stop - Stops all running monitor objects
func (c *NmonClient) Stop() {
	c.Logging.Debug("stopping all monitoring objects")
	for key := range runningPingObjects {
		c.Logging.Tracef("tring to stop ping:%v object", key)
		runningPingObjects[key].Notify <- "done"
		c.Logging.Tracef("stopped ping:%v object", key)
	}
	for key := range runningResolveObjects {
		c.Logging.Tracef("tring to stop resolve:%v object", key)
		runningResolveObjects[key].Notify <- "done"
		c.Logging.Tracef("stopped resolve:%v object", key)
	}
	for key := range runningTraceObjects {
		c.Logging.Tracef("tring to stop trace:%v object", key)
		runningTraceObjects[key].Notify <- "done"
		c.Logging.Tracef("stopped trace:%v object", key)
	}
	c.Logging.Debug("stopped all monitoring objects")
}
