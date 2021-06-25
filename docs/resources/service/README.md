# Load Balancer Service

There are two types of load balancer services supported, version 1.0 or 2.0.
Version 1.0 and 2.0 are both Layer 4 load balancers that live only in the
Linux kernel space. Version 1.0 uses Linux iptables, while version 2.0 uses the
Linux kernel's IP Virtual Server (IPVS) for load balancing. Both versions of the
load balancer run inside the cluster as a deployment in the `ibm-system` namespace
with two replicas that run [Keepalived](https://www.keepalived.org/). Therefore,
the available capacity of the load balancers is always dedicated to your own
cluster. Additionally, both versions do not terminate the connection. Instead,
they forward connections to an app pod.

Refer to the example load balancer setup YAML for the required cluster
resources that must be setup before creating load balancer services. The setup
is required to run the load balancer components within the cluster. The
components rely on [Keepalived](https://www.keepalived.org/) for managing load
balancer virtual IP addresses and [Calico](https://www.projectcalico.org/) for
managing network policies. Refer to the annotations documentation for load
balancer service configuration.

References:
- [Calico](https://www.projectcalico.org/)
- [Create an External Load Balancer](http://kubernetes.io/docs/user-guide/load-balancer/)
- [Keepalived](https://www.keepalived.org/)
