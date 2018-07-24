# Prometheus-Swarm service discovery

This is a for of https://github.com/ContainerSolutions/prometheus-swarm-discovery which aims at providing a
viable long-term discovery service solution.

This fork includes commits from numerous other forks, as well
as additional code and alterations made by its maintainer.

## Configuration

## Deployment

The only thing required to run Prometheus and the discovery tool is to launch a Swarm stack using the provided docker-compose.yaml
file.

> deploying prometheus with swarm-discovery

```shell
docker stack deploy -c docker-compose.yaml prometheus
```

## Deploying services

> sample/docker-compose.yml

```yaml
version: '3'

# Add the prometheus network as an external network
networks:
  prometheus:
    external:
      name: prometheus_net

services:
  front-end:
    image: my-app:latest
    # Enable prometheus monitoring, and expose a port (default: 8080)
    labels:
      prometheus.enable: true
      prometheus.port: 8079
      prometheus.path: /path/to/metrics # (optional, default: /metrics)
    # Make sure to connect to the prometheus network!
    networks:
      - default
      - prometheus
```

## Metadata labels

The discovery tool attaches a set of metadata labels to each target that are available during the [relabeling phase](https://prometheus.io/docs/operating/configuration/#<relabel_config>) of the service discovery in Prometheus:

- `__meta_docker_service_label_<labelname>`: The value of this service label.
- `__meta_docker_task_label_<labelname>`: The value of this task label.
- `__meta_docker_task_name`: The name of the Docker task.

Labels starting with `__` are removed after the relabeling phase, so that these labels will not show up on time series directly.

## Configuration options

```shell
$ ./prometheus-swarm discover --help
Starts Swarm service discovery

Usage:
  promswarm discover [flags]

Flags:
  -c, --clean               Disconnects unused networks from the Prometheus container, and deletes them. (default true)
  -i, --interval int        The interval, in seconds, at which the discovery process is kicked off (default 30)
  -l, --loglevel string     Specify log level: debug, info, warn, error (default "info")
  -o, --output string       Output file that contains the Prometheus endpoints. (default "swarm-endpoints.json")
  -p, --prometheus string   Name of the Prometheus service (default "prometheus")
```
