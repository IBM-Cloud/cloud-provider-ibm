<!-- markdownlint-disable MD013 -->
# IBM Cloud Provider

This is the IBM Cloud Provider repository which implements the
IBM Cloud Controller Manager (CCM). The IBM CCM can be used to provide IBM Cloud
infrastructure node and load balancer support to
[Kubernetes](https://kubernetes.io/docs/home/) or
[OpenShift](https://docs.openshift.com/) clusters running on
[IBM Cloud](https://cloud.ibm.com/docs). This repository branch is based on
[Kubernetes version v1.34.0-rc.2](https://github.com/kubernetes/kubernetes/tree/v1.34.0-rc.2).

See [CONTRIBUTING.md](./CONTRIBUTING.md) for contribution guidelines.

## Unit Testing

The [GO GitHub Action](.github/workflows/go.yml) workflow will run the GO unit tests on pull requests.
The GO unit tests can also be invoked locally by running:

`make test`

## Dependencies

GO library dependencies are managed by [Dependabot](.github/dependabot.yml).

## Kubernetes Patch Update Process

The [kube-update GitHub Action](.github/workflows/kube-update.yml) workflow will detect Kubernetes updates
and create pull requests to update the required files.
