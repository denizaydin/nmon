package configserver

import (
	proto "dnzydn.com/nmon/api"
	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

//GetData Return configuration data
func GetData(logging *logrus.Logger) *map[string]*proto.MonitoringObject {
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
		logging.Println("config file changed:%v", e.Name)
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
		monitoringObjects = newmonitoringObjects

		for k := range monitoringObjects {
			delete(monitoringObjects, k)
		}

		logging.Tracef("changed configuration data to %v", monitoringObjects)

	})
	logging.Infof("using config: %s\n", viper.ConfigFileUsed())
	if err := viper.ReadInConfig(); err != nil {
		logging.Errorf("can not read config file, %s", err)
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
	}
	for traceDest := range tracedestinations {
		monitoringObjects[traceDest] = &proto.MonitoringObject{
			Updatetime: 0,
			Object:     &proto.MonitoringObject_Tracedest{Tracedest: tracedestinations[traceDest]},
		}
	}
	for resolveDest := range resolvedestinations {
		monitoringObjects[resolveDest] = &proto.MonitoringObject{
			Object: &proto.MonitoringObject_Resolvedest{
				Resolvedest: resolvedestinations[resolveDest],
			},
		}
	}
	logging.Tracef("returning configuration data %v", monitoringObjects)
	return &monitoringObjects
}
