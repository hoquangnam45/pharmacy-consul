package main

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"time"

	"github.com/hoquangnam45/pharmacy-common-go/helper/dns"
	"github.com/hoquangnam45/pharmacy-common-go/helper/ecs"
	handler "github.com/hoquangnam45/pharmacy-common-go/helper/errorHandler"
)

const CONSUL_PEERS_URL_ENV = "CONSUL_PEERS_URL"
const CONSUL_CONFIG_PATH_ENV = "CONSUL_CONFIG_PATH"
const ECS_CONTAINER_METADATA_FILE_ENV = "ECS_CONTAINER_METADATA_FILE"

func main() {
	containerInfo, err := handler.Lift(ecs.GetContainerInfo)(os.Getenv(ECS_CONTAINER_METADATA_FILE_ENV)).
		RetryUntilSuccess(time.Duration(30)*time.Second, time.Duration(5)*time.Second).
		EvalNoCleanup()
	if err != nil {
		log.Fatal(err)
		return
	}
	handler.FlatMap2(
		handler.Just(os.Getenv(CONSUL_PEERS_URL_ENV)),
		func(peerUrl string) *handler.MaybeError[map[string]bool] {
			return handler.Lift(dns.ResolveSrvDns)(peerUrl).
				RetryUntilSuccess(time.Duration(50)*time.Second, time.Duration(10)*time.Second)
		},
		handler.Lift(func(peers map[string]bool) (any, error) {
			consulConfigPath := getEnvOrDefault(CONSUL_CONFIG_PATH_ENV, "/consul/config/consul_config.json")
			return nil, writeToConsulConfig(consulConfigPath, containerInfo, peers)
		}),
	).GetWithHandler(func(err error) {
		log.Fatal(err)
	})
}

func getEnvOrDefault(key, defaultValue string) string {
	env, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}
	return env
}

func writeToConsulConfig(configPath string, containerInfo *ecs.ContainerInfo, peers map[string]bool) error {
	consulConfig := map[string]any{}
	startJoins := []string{}

	for k := range peers {
		startJoins = append(startJoins, k)
	}
	consulConfig["retry_join"] = startJoins
	consulConfig["client_addr"] = "0.0.0.0"
	consulConfig["rejoin_after_leave"] = true
	consulConfig["leave_on_terminate"] = true
	consulConfig["log_level"] = "INFO"
	consulConfig["ui_config"] = map[string]any{
		"enabled": true,
	}
	consulConfig["server"] = true
	consulConfig["bootstrap_expect"] = 1
	consulConfig["log_file"] = "/var/log/consul/"
	consulConfig["auto_reload_config"] = true
	consulConfig["data_dir"] = "/consul/data/"
	consulConfig["bind_addr"] = "{{ GetInterfaceIP \"eth0\" }}"
	consulConfig["advertise_addr"] = containerInfo.HostIp
	consulConfig["ports"] = map[string]int{
		"serf_lan": containerInfo.PortMappings[8301],
		"serf_wan": containerInfo.PortMappings[8302],
		"dns":      containerInfo.PortMappings[8600],
		"http":     containerInfo.PortMappings[8500],
		"server":   containerInfo.PortMappings[8300],
	}
	consulConfig["client_addr"] = "0.0.0.0"
	consulConfig["disable_update_check"] = true
	for k, v := range containerInfo.PortMappings {
		log.Println(k, ":", v)
	}
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
