package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/hoquangnam45/pharmacy-common-go/helper/ecs"
	"github.com/hoquangnam45/pharmacy-common-go/util"
	h "github.com/hoquangnam45/pharmacy-common-go/util/errorHandler"
)

const CONSUL_CONFIG_PATH = "/consul/config/consul_config.json"
const ECS_CONSUL_SERVER_URL_ENV = "ECS_CONSUL_SERVER_URL"
const CONSUL_BIND_INTERFACE_ENV = "CONSUL_BIND_INTERFACE"

func main() {
	advertiseIp := ""
	bindInterface := ""
	consulServers := map[string]bool{}
	if bindInterface_, ok := os.LookupEnv("CONSUL_BIND_INTERFACE"); ok {
		bindInterface = bindInterface_
		advertiseIp = h.Lift(util.FindBindInterfaceAddress)(bindInterface_).PanicEval()
	} else {
		pair := h.FactoryM(util.FindFirstNonLoopBackAddr).PanicEval()
		bindInterface = pair.Second
		advertiseIp = pair.First
		util.SugaredLogger.Infof("Bind to interface %s with address %s", bindInterface, advertiseIp)
	}

	if consulServerUrl, ok := os.LookupEnv(ECS_CONSUL_SERVER_URL_ENV); ok {
		consulServers = h.FactoryM(func() (map[string]bool, error) {
			return ecs.ResolveHostModeService(context.Background(), consulServerUrl)
		}).
			RetryUntilSuccess(time.Second*50, time.Second*10).
			PanicEval()
	}
	consulServers[advertiseIp] = true

	serverMode := util.GetEnvOrDefaultT("CONSUL_SERVER_MODE", strconv.ParseBool, true)
	bootstrapExpect := int(util.GetEnvOrDefaultT("CONSUL_SERVER_BOOTSTRAP_EXPECT", func(s string) (uint64, error) {
		return strconv.ParseUint(s, 10, 0)
	}, 1))

	h.FactoryM(func() (any, error) {
		return nil, writeToConsulConfig(CONSUL_CONFIG_PATH, serverMode, bootstrapExpect, bindInterface, consulServers)
	}).PanicEval()
}

func writeToConsulConfig(configPath string, serverMode bool, bootstrapExpect int, bindInterface string, consulServers map[string]bool) error {
	consulConfig := map[string]any{}

	consulConfig["client_addr"] = "0.0.0.0"
	consulConfig["rejoin_after_leave"] = true
	consulConfig["leave_on_terminate"] = true
	consulConfig["log_level"] = "INFO"
	consulConfig["ui_config"] = map[string]any{
		"enabled": true,
	}
	consulConfig["bind_addr"] = fmt.Sprintf("{{ GetInterfaceIP \"%s\" }}", bindInterface)
	consulConfig["server"] = serverMode
	if serverMode {
		consulConfig["bootstrap_expect"] = bootstrapExpect
	}
	consulConfig["retry_join"] = util.SetToList(consulServers)
	consulConfig["auto_reload_config"] = true
	consulConfig["data_dir"] = "/consul/data/"
	consulConfig["client_addr"] = "0.0.0.0"
	consulConfig["disable_update_check"] = true
	bytes, err := json.Marshal(consulConfig)
	if err != nil {
		return err
	}
	tempFile, err := os.CreateTemp("/tmp", "consul-config-*.json")
	if err != nil {
		return err
	}
	err = os.WriteFile(tempFile.Name(), bytes, 0644)
	if err != nil {
		return err
	}
	configFile, err := os.Create(configPath)
	if err != nil {
		return err
	}
	_, err = io.Copy(configFile, tempFile)
	if err != nil {
		return err
	}
	return nil
}
