package main

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/hoquangnam45/pharmacy-common-go/helper/errorHandler"
)

const CONSUL_PEERS_URL_ENV = "CONSUL_PEERS_URL"
const ECS_CONTAINER_METADATA_FILE_ENV = "ECS_CONTAINER_METADATA_FILE"
const CONSUL_CONFIG_PATH_ENV = "CONSUL_CONFIG_PATH"
const CONSUL_HTTP_API_PORT = 8500

type UrlPart struct {
	Prefix string
	Domain string
	Port   int
	Path   string
}

func main() {
	consulConfigPath := getEnvOrDefault(CONSUL_CONFIG_PATH_ENV, func(val string) (string, error) {
		return val, nil
	}, "/consul/config/consul_config.json")
	peers, err := resolveSrvDns(os.Getenv(CONSUL_PEERS_URL_ENV))
	if err != nil {
		log.Fatal(err)
		return
	}
	err = writeToConsulConfig(consulConfigPath, peers)
	if err != nil {
		log.Fatal(err)
		return
	}
}

func getEnvOrDefault[T any](key string, convert func(string) (T, error), defaultValue T) T {
	env, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}
	v, err := convert(env)
	if err != nil {
		return defaultValue
	}
	return v
}

func writeToConsulConfig(configPath string, peers map[string]bool) error {
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
	consulConfig["advertise_addr"] = "{{ GetInterfaceIP \"eth0\" }}"
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

func resolveSrvDns(link string) (map[string]bool, error) {
	addrs, _, err := errorHandler.FlatMap(
		errorHandler.FlatMap(
			errorHandler.Just(link),
			func(host string) *errorHandler.MaybeError[[]*net.SRV] {
				log.Printf("Start lookingup host %s", host)
				_, addrs, err := net.LookupSRV("", "", host)
				if err != nil {
					return errorHandler.Error[[]*net.SRV](err)
				}
				return errorHandler.Just(addrs)
			}).RetryUntilSuccess(time.Second*time.Duration(50), time.Second*time.Duration(10)),
		func(addrs []*net.SRV) *errorHandler.MaybeError[map[string]bool] {
			resolvedAddrs := map[string]bool{}
			log.Printf("Found %d records: ", len(addrs))
			for _, v := range addrs {
				resolvedAddr := v.Target + ":" + strconv.Itoa(int(v.Port))
				log.Print(resolvedAddr)
				resolvedAddrs[resolvedAddr] = true
			}
			return errorHandler.Just(resolvedAddrs)
		},
	).Eval()
	return addrs, err
}
