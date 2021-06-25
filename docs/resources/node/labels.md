# Node Labels

| Label | Description |
| --- | --- |
| `ibm-cloud.kubernetes.io/external-ip` | Node external (public) IP address (optional) |
| `ibm-cloud.kubernetes.io/internal-ip` | Node internal (private) IP address |
| `ibm-cloud.kubernetes.io/machine-type` | Node machine (instance) type, same as `node.kubernetes.io/instance-type` (replaces `beta.kubernetes.io/instance-type` label) |
| `ibm-cloud.kubernetes.io/region` | Node region, same as `topology.kubernetes.io/region` (replaces `failure-domain.beta.kubernetes.io/region` label) |
| `ibm-cloud.kubernetes.io/zone` | Node availability zone, same as `topology.kubernetes.io/zone` label (replaces `failure-domain.beta.kubernetes.io/zone` label) |
| `ibm-cloud.kubernetes.io/worker-id` | Node worker ID |
| `privateVLAN` | Node private VLAN ID |
| `publicVLAN` | Node public VLAN ID (optional) |
