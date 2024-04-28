[![CI](https://github.com/projectsveltos/ui-backend/actions/workflows/main.yaml/badge.svg)](https://github.com/projectsveltos/ui-backend/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/projectsveltos/ui-backend)](https://goreportcard.com/report/github.com/projectsveltos/ui-backend)
[![Release](https://img.shields.io/github/v/release/projectsveltos/ui-backend)](https://github.com/projectsveltos/ui-backend/releases)
[![Slack](https://img.shields.io/badge/join%20slack-%23projectsveltos-brighteen)](https://join.slack.com/t/projectsveltos/shared_invite/zt-1hraownbr-W8NTs6LTimxLPB8Erj8Q6Q)
[![License](https://img.shields.io/badge/license-Apache-blue.svg)](LICENSE)
[![Twitter Follow](https://img.shields.io/twitter/follow/projectsveltos?style=social)](https://twitter.com/projectsveltos)

<img src="https://raw.githubusercontent.com/projectsveltos/sveltos/main/docs/assets/logo.png" width="200">

Please refere to sveltos [documentation](https://projectsveltos.github.io/sveltos/).

## What this repository is
This repo contains a service that exposes all APIs used by Sveltos frontend.

### Get ClusterAPI powered clusters

```/capiclusters```


It is possible to filter by:

. ```namespace=<string>``` => returns only ClusterAPI powered clusters in a namespace whose name contains the speficied string

. ```name=<string>``` => returns only ClusterAPI powered clusters whose name contains the speficied string

. ```labels=<key1=value1,key2=value2>``` => returns only ClusterAPI powered clusters whose label matches the specified label selector

For instance:

```
http://localhost:9000/capiclusters?limit=1&skip=0&namespace=default&labels=env:fv,cluster.x-k8s.io%2Fcluster-name:clusterapi-workload
```

returns 

```
{"totalClusters":1,"managedClusters":[{"namespace":"default","name":"clusterapi-workload","clusterInfo":{"labels":{"cluster.x-k8s.io/cluster-name":"clusterapi-workload","env":"fv","topology.cluster.x-k8s.io/owned":""},"version":"v1.27.0","ready":true,"failureMessage":null}}]}
```

This API supports pagination. Use:

. ```limit=<int>``` to specify the number of ClusterAPI powered clusters the API will return

. ```skip=<int>``` to specify from which ClusterAPI powered cluster to start (Clusters are ordered by namespace/name)

### Get SveltosClusters

```/sveltosclusters```


It is possible to filter by:

. ```namespace=<string>``` => returns only SveltosClusters in a namespace whose name contains the speficied string

. ```name=<string>``` => returns only SveltosClusters whose name contains the speficied string

. ```labels=<key1=value1,key2=value2>``` => returns only SveltosClusters whose label matches the specified label selector

For instance:

```
http://localhost:9000/sveltosclusters?limit=1&skip=0&namespace=mgmt&name=mgmt
```

returns 

```
{"totalClusters":1,"managedClusters":[{"namespace":"mgmt","name":"mgmt","clusterInfo":{"labels":null,"version":"v1.29.0","ready":true,"failureMessage":null}}]}
```

This API supports pagination. Use:

. ```limit=<int>``` to specify the number of SveltosClusters the API will return

. ```skip=<int>``` to specify from which SveltosClusters to start (Clusters are ordered by namespace/name)

### Get Helm Releases deployed in a cluster

```/helmcharts?namespace=<namespace>&name=<cluster-name>&type=<cluster type>```

where cluster type can either be __capi__ for ClusterAPI powered clusters or __sveltos__ for SveltosClusters

This API supports pagination. Use:

. ```limit=<int>``` to specify the number of helm releases the API will return

. ```skip=<int>``` to specify from which Helm release to start (Helm Releases are ordered by lastAppliedTime)

For instance:

```
http://localhost:9000/helmcharts?namespace=default&name=clusterapi-workload&type=capi
```

returns 

```
{"totalHelmReleases":4,"helmReleases":[{"repoURL":"https://prometheus-community.github.io/helm-charts","releaseName":"prometheus","namespace":"prometheus","chartVersion":"23.4.0","icon":"https://raw.githubusercontent.com/prometheus/prometheus.github.io/master/assets/prometheus_logo-cb55bb5c346.png","lastAppliedTime":"2024-04-28T13:49:29Z","profileName":"ClusterProfile/prometheus-grafana"},{"repoURL":"https://grafana.github.io/helm-charts","releaseName":"grafana","namespace":"grafana","chartVersion":"6.58.9","icon":"https://raw.githubusercontent.com/grafana/grafana/master/public/img/logo_transparent_400x.png","lastAppliedTime":"2024-04-28T13:49:38Z","profileName":"ClusterProfile/prometheus-grafana"},{"repoURL":"https://kyverno.github.io/kyverno/","releaseName":"kyverno-latest","namespace":"kyverno","chartVersion":"3.1.4","icon":"https://github.com/kyverno/kyverno/raw/main/img/logo.png","lastAppliedTime":"2024-04-28T13:49:32Z","profileName":"ClusterProfile/deploy-kyverno"},{"repoURL":"https://helm.nginx.com/stable/","releaseName":"nginx-latest","namespace":"nginx","chartVersion":"1.1.3","icon":"https://raw.githubusercontent.com/nginxinc/kubernetes-ingress/v3.4.3/charts/nginx-ingress/chart-icon.png","lastAppliedTime":"2024-04-28T13:49:36Z","profileName":"ClusterProfile/nginx"}]}
```
### Get Kubernetes Resources deployed in a cluster

```/resources?namespace=<namespace>&name=<cluster-name>&type=<cluster type>```

where cluster type can either be __capi__ for ClusterAPI powered clusters or __sveltos__ for SveltosClusters

This API supports pagination. Use:

. ```limit=<int>``` to specify the number of Kubernetes resources the API will return

. ```skip=<int>``` to specify from which Kubernetes resources to start (Kubernetes resources are ordered by lastAppliedTime)

For instance:

```
http://localhost:9000/resources?namespace=default&name=clusterapi-workload&type=capi
```

returns

```
{"totalResources":2,"resources":[{"name":"nginx","group":"","kind":"Namespace","version":"v1","lastAppliedTime":"2024-04-28T13:52:26Z","profileNames":["ClusterProfile/deploy-resources"]},{"name":"","group":"","kind":"","version":"","profileNames":null}]}
```

## Contributing 

❤️ Your contributions are always welcome! If you want to contribute, have questions, noticed any bug or want to get the latest project news, you can connect with us in the following ways:

1. Open a bug/feature enhancement on github [![contributions welcome](https://img.shields.io/badge/contributions-welcome-brightgreen.svg?style=flat)](https://github.com/projectsveltos/sveltos-manager/issues)
2. Chat with us on the Slack in the #projectsveltos channel [![Slack](https://img.shields.io/badge/join%20slack-%23projectsveltos-brighteen)](https://join.slack.com/t/projectsveltos/shared_invite/zt-1hraownbr-W8NTs6LTimxLPB8Erj8Q6Q)
3. [Contact Us](mailto:support@projectsveltos.io)

## License

Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
