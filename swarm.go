package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"strconv"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/util/strutil"
	"github.com/spf13/cobra"
)

const (
	portLabel    = "prometheus.port"
	pathLabel    = "prometheus.path"
	includeLabel = "prometheus.enable"
)

var logger = logrus.New()
var options = Options{}

// allocateIP returns the 3rd last IP in the network range.
func allocateIP(netCIDR *net.IPNet) string {
	allocIP := net.IP(make([]byte, 4))
	for i := range netCIDR.IP {
		allocIP[i] = netCIDR.IP[i] | ^netCIDR.Mask[i]
	}

	allocIP[3] = allocIP[3] - 2
	return allocIP.String()
}

func writeSDConfig(scrapeTasks []scrapeTask, output string) {
	jsonScrapeConfig, err := json.MarshalIndent(scrapeTasks, "", "  ")
	if err != nil {
		panic(err)
	}

	logger.Debug("Writing Prometheus config file")

	err = ioutil.WriteFile(output, jsonScrapeConfig, 0644)
	if err != nil {
		panic(err)
	}
}

func findPrometheusContainer(serviceName string) (*swarm.Task, error) {
	cli, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}

	taskFilters := filters.NewArgs()
	taskFilters.Add("desired-state", string(swarm.TaskStateRunning))
	taskFilters.Add("service", serviceName)

	promTasks, err := cli.TaskList(context.Background(), types.TaskListOptions{Filters: taskFilters})
	if err != nil {
		return nil, err
	}

	if len(promTasks) == 0 || promTasks[0].Status.ContainerStatus.ContainerID == "" {
		return nil, fmt.Errorf("Could not find container for service %s", serviceName)
	}

	return &promTasks[0], nil
}

type scrapeService struct {
	ServiceName string
	scrapeTasks scrapeTask
}

type scrapeTask struct {
	Targets []string
	Labels  map[string]string
}

// collectPorts builds a map of ports collected from container exposed ports and/or from ports defined
// as container labels
func collectPorts(service swarm.Service) map[int]struct{} {
	ports := make(map[int]struct{})

	// collects port defined in the service's labels
	if portstr, ok := service.Spec.Labels[portLabel]; ok {
		if port, err := strconv.Atoi(portstr); err == nil {
			ports[port] = struct{}{}
		}
	}

	return ports
}

func collectIPs(prometheusTask *swarm.Task, task swarm.Task) ([]net.IP, map[string]swarm.Network) {
	var containerIPs []net.IP
	taskNetworks := make(map[string]swarm.Network)

	for _, netatt := range task.NetworksAttachments {
		if netatt.Network.Spec.Name == "ingress" || netatt.Network.DriverState.Name != "overlay" {
			continue
		}

		for _, promNet := range prometheusTask.NetworksAttachments {
			if promNet.Network.ID != netatt.Network.ID {
				continue
			}

			for _, ipcidr := range netatt.Addresses {
				ip, _, err := net.ParseCIDR(ipcidr)
				if err != nil {
					logger.Error(err)
					continue
				}

				containerIPs = append(containerIPs, ip)
				taskNetworks[netatt.Network.ID] = netatt.Network

				return containerIPs, taskNetworks
			}
		}
	}

	return containerIPs, taskNetworks
}

func taskLabels(task swarm.Task, service swarm.Service) map[string]string {
	labels := map[string]string{
		model.JobLabel:                                      service.Spec.Name,
		model.MetaLabelPrefix + "docker_task_name":          task.Name,
		model.MetaLabelPrefix + "docker_task_desired_state": string(task.DesiredState),
	}

	// Add path
	if path, ok := service.Spec.Labels[pathLabel]; ok {
		labels[model.MetricsPathLabel] = path
	}

	// Sanitize other labels
	for k, v := range task.Labels {
		labels[strutil.SanitizeLabelName(model.MetaLabelPrefix+"docker_task_label_"+k)] = v
	}
	for k, v := range service.Spec.Labels {
		labels[strutil.SanitizeLabelName(model.MetaLabelPrefix+"docker_service_label_"+k)] = v
	}

	return labels
}

func discoverSwarm(prometheusTask *swarm.Task, outputFile string) {
	cli, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}

	// Find all labeled services
	serviceFilters := filters.NewArgs()
	serviceFilters.Add("label", includeLabel+"=true")
	services, err := cli.ServiceList(context.Background(), types.ServiceListOptions{Filters: serviceFilters})
	if err != nil {
		panic(err)
	}

	var scrapeTasks []scrapeTask
	allNetworks := make(map[string]swarm.Network)

	for _, service := range services {
		// Find tasks for services
		taskFilters := filters.NewArgs()
		taskFilters.Add("service", service.ID)
		taskFilters.Add("desired-state", string(swarm.TaskStateRunning))

		tasks, err := cli.TaskList(context.Background(), types.TaskListOptions{Filters: taskFilters})
		if err != nil {
			panic(err)
		}

		for _, task := range tasks {
			logger.Debugf("Task %s should be scanned by Prometheus", task.ID)

			ports := collectPorts(service)
			containerIPs, taskNetworks := collectIPs(prometheusTask, task)
			var taskEndpoints []string

			for k, v := range taskNetworks {
				allNetworks[k] = v
			}

			// if exposed ports are found, or ports defined through labels, add them to the Prometheus target.
			// if not, add only the container IP as a target, and Prometheus will use the default port (80).
			for _, ip := range containerIPs {
				if len(ports) > 0 {
					for port := range ports {
						taskEndpoints = append(taskEndpoints, fmt.Sprintf("%s:%d", ip.String(), port))
					}
				} else {
					taskEndpoints = append(taskEndpoints, ip.String())
				}
			}

			logger.Debugf("Found task %s with IPs %s", task.ID, taskEndpoints)

			scrapetask := scrapeTask{
				Targets: taskEndpoints,
				Labels:  taskLabels(task, service),
			}

			scrapeTasks = append(scrapeTasks, scrapetask)
		}
	}

	writeSDConfig(scrapeTasks, outputFile)
}

func discoveryProcess(cmd *cobra.Command, args []string) {
	level, err := logrus.ParseLevel(options.logLevel)
	if err != nil {
		logger.Fatal(err)
	}
	logger.Level = level

	logger.Info("Starting service discovery process using Prometheus service [", options.prometheusService, "]")

	for {
		time.Sleep(time.Duration(options.discoveryInterval) * time.Second)
		prometheusContainer, err := findPrometheusContainer(options.prometheusService)
		if err != nil {
			logger.Warn(err)
			continue
		}

		discoverSwarm(prometheusContainer, options.output)
	}
}

// Options structure for all the cmd line flags
type Options struct {
	prometheusService string
	discoveryInterval int
	logLevel          string
	output            string
	clean             bool
}

func main() {
	var cmdDiscover = &cobra.Command{
		Use:   "discover",
		Short: "Starts Swarm service discovery",
		Run:   discoveryProcess,
	}

	cmdDiscover.Flags().StringVarP(&options.prometheusService, "prometheus", "p", "prometheus", "Name of the Prometheus service")
	cmdDiscover.Flags().IntVarP(&options.discoveryInterval, "interval", "i", 30, "The interval, in seconds, at which the discovery process is kicked off")
	cmdDiscover.Flags().StringVarP(&options.logLevel, "loglevel", "l", "info", "Specify log level: debug, info, warn, error")
	cmdDiscover.Flags().StringVarP(&options.output, "output", "o", "swarm-endpoints.json", "Output file that contains the Prometheus endpoints.")
	cmdDiscover.Flags().BoolVarP(&options.clean, "clean", "c", true, "Disconnects unused networks from the Prometheus container, and deletes them.")

	var rootCmd = &cobra.Command{Use: "promswarm"}
	rootCmd.AddCommand(cmdDiscover)
	rootCmd.Execute()
}
