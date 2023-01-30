package main

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hoquangnam45/pharmacy-common-go/helper/errorHandler"
)

const CONSUL_PEERS_SRV_URL_ENV = "CONSUL_PEERS_SRV_URL"
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
	hostPrivateIp, hostPort, ok := getHostIpAndPortFromContainerPortWithEcsMetadataFile(os.Getenv(ECS_CONTAINER_METADATA_FILE_ENV), CONSUL_HTTP_API_PORT, 20)
	if !ok {
		log.Fatal("Check if ecs metadata structure or path is correct or not, or recheck the task definition for correct port mappings")
		return
	}
	peers, err := resolveSrvDns(os.Getenv(CONSUL_PEERS_SRV_URL_ENV))
	if err != nil {
		log.Fatal(err)
		return
	}
	err = writeToConsulConfig(consulConfigPath, hostPrivateIp, hostPort, peers)
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

func writeToConsulConfig(configPath string, hostPrivateIp string, hostPort int, peers map[string]bool) error {
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
	consulConfig["ui"] = true
	consulConfig["server"] = true
	consulConfig["bootstrap_expect"] = 0
	consulConfig["log_file"] = "/var/log/consul/"
	consulConfig["advertise_addr"] = hostPrivateIp
	consulConfig["auto_reload_config"] = true
	consulConfig["ports"] = map[string]int{
		"http": hostPort,
	}
	consulConfig["data_dir"] = "/var/lib/consul/"

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

func getHostIpAndPortFromContainerPortWithEcsMetadataFile(path string, containPort int, timeoutInSecond int) (string, int, bool) {
	startTime := time.Now().Unix()
	for int(time.Now().Unix()-startTime) <= timeoutInSecond {
		metadata, err := readJson(path)
		if err != nil {
			time.Sleep(time.Millisecond * time.Duration(500))
			continue
		}
		hostPrivateIp, hostPort, ok := getIpAndPort(metadata, containPort)
		if ok {
			return hostPrivateIp, hostPort, true
		}
	}
	return "", 0, false
}

func readJson(path string) (map[string]any, error) {
	ret, cleanup, err := errorHandler.FlatMap2(
		errorHandler.Lift(os.Open)(path).Cleanup(func(f *os.File) {
			if f != nil {
				f.Close()
			}
		}),
		func(f *os.File) *errorHandler.MaybeError[[]byte] {
			return errorHandler.Lift(io.ReadAll)(f)
		},
		func(bytes []byte) *errorHandler.MaybeError[map[string]any] {
			unmarshallF := func([]byte) (map[string]any, error) {
				ret := map[string]any{}
				err := json.Unmarshal(bytes, &ret)
				return ret, err
			}
			return errorHandler.Lift(unmarshallF)(bytes)
		},
	).Eval()
	defer cleanup()
	return ret, err
}

func getIpAndPort(metadata map[string]any, port int) (string, int, bool) {
	if status, ok := metadata["MetadataFileStatus"].(string); ok {
		if strings.ToLower(status) == "ready" {
			hostPrivateIp := metadata["HostPrivateIPv4Address"].(string)
			portMappings := metadata["PortMappings"].([]interface{})
			for _, v := range portMappings {
				portMapping, _ := v.(map[string]any)
				containerPort := int(portMapping["ContainerPort"].(float64))
				hostPort := int(portMapping["HostPort"].(float64))
				if containerPort == port {
					return hostPrivateIp, hostPort, true
				}
			}
		}
	}
	return "", 0, false
}

func resolveSrvDns(link string) (map[string]bool, error) {
	addrs, _, err := errorHandler.FlatMap2(
		errorHandler.Just(link),
		func(host string) *errorHandler.MaybeError[[]*net.SRV] {
			log.Printf("Start lookingup host %s", host)
			_, addrs, err := net.LookupSRV("", "", host)
			if err != nil {
				return errorHandler.Error[[]*net.SRV](err)
			}
			return errorHandler.Just(addrs)
		},
		func(addrs []*net.SRV) *errorHandler.MaybeError[map[string]bool] {
			resolvedAddrs := map[string]bool{}
			for _, v := range addrs {
				resolvedAddr := v.Target + ":" + strconv.FormatUint(uint64(v.Port), 10)
				resolvedAddrs[resolvedAddr] = true
			}
			return errorHandler.Just(resolvedAddrs)
		},
	).Eval()
	return addrs, err
}
