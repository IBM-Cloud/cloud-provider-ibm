# IBM Cloud Provider

This is the IBM Cloud Provider repository which implements the
IBM Cloud Controller Manager (CCM). The IBM CCM can be used to provide IBM Cloud
infrastructure node and load balancer support to
[Kubernetes](https://kubernetes.io/docs/home/) or
[OpenShift](https://docs.openshift.com/) clusters running on
[IBM Cloud](https://cloud.ibm.com/docs). This repository branch is based on
[Kubernetes version v1.24.1](https://github.com/kubernetes/kubernetes/tree/v1.24.1).
See [CONTRIBUTING.md](./CONTRIBUTING.md) for contribution guidelines.

## Local Build and Deploy Instructions

### Building IBM Cloud Provider from your Local Repository

These build instructions have been verified using
[VirtualBox version 6.1.18](https://www.virtualbox.org/wiki/Downloads),
[Vagrant version 2.2.14](https://www.vagrantup.com/downloads) and MacOS version 11.4.

1. Change to your local repository. The build will work against this directory
   by making it a vagrant folder synchronized to the VM.

1. `cd vagrant-kube-build`

1. `./build.sh`

1. If the build fails, you can correct the errors and re-run `./build.sh`.
   You can also run specific build steps by passing one or more of the following
   options:

   * Build setup (always run with other steps to setup the build environment and dependencies): `setup`
   * Build source (i.e. `make fmt`, `make lint`, etc.): `source`
   * Build containers (i.e. `make containers`): `containers`
   * Build Docker registry for the containers: `registry`
   * Build specific make step (cannot be run with other steps): `make [[option] ...]`

1. Once the build is complete, you can log into the VM via `vagrant ssh`.
   Running `vagrant ssh-config` will give you SSH configuration updates which
   you can apply to your host's `~/.ssh/config` file. This allows you to run
   `ssh cloud-provider-ibm-build` to access the VM. Also, you can use
   `vagrant suspend` to suspend the VM and `vagrant destroy -f` to destroy it.

### Deploying IBM Cloud Provider

Refer to [examples](./docs/examples) for deploying the IBM Cloud Provider based
on underlying infrastructure type, classic or VPC.

## Testing

### Unit

The default build process above will run all unit tests and matches
what is done by Travis CI.

`make test`

### Functional

Functional tests are available under `tests/fvt` and are designed to run against
an existing cluster. These tests exercise the load balancer interfaces without
having to do a full build and deployment. These tests are **not** run by
Travis CI.

To run these tests, do the following:

1. Copy your cluster admin configuration into `tests/fvt/kubeconfig` along with
   the associated 3 `*.pem` files:
    1. The `setupFVT.sh` script in `vagrant-kube-build` can do this for you, just:
        - `cd vagrant-kube-build`
        - `./setupFVT.sh <CLUSTER_NAME>`

1. Edit `ibm-cloud-config.ini` file depending on whether you are testing a cluster
   using classic or VPC infrastructure:
    1. For Classic, no modifications are needed
    1. For VPC, you must uncomment the lines at the bottom from `[provider]` to
       the end, and update them with information from your cluster:
        - `accountID` can be anything (for example, `accountID = bogusAccountID`)
        - `clusterID` must be set to your cluster ID

1. Once you have this done, the default `./build.sh` script will run the FVT tests.

1. You can also use this tester to run additional tests.  To do that:
    1. Run the full `./build.sh` to run through the default tests once
    1. `vagrant ssh` into the build VM and run other variations of the tests
       using any of the following as examples:
        - `make runfvt TEST_FVT_OPTIONS="--action=create --name=BradsLB1"`
        - `make runfvt TEST_FVT_OPTIONS="--action=delete --name=BradsLB1"`

## Dependencies

Dependencies are managed via [go modules](https://github.com/golang/go/wiki/Modules)
for builds. Be sure to regenerate the `go.mod` and `go.sum` files when there are
new or updated dependencies. You can do this by running `make updatedeps`.

## Kubernetes Version Update Process

The following steps are required to create a new or update an existing branch
for a new Kubernetes version.

1. If the update is for a new Kubernetes major or minor version, select the
   current branch from which the new branch will be created. Then
   in the `Find or create a branch...` field under the `Branch` drop-down menu,
   enter the new branch name `release-<major>.<minor>` where `<major>.<minor>`
   is the Kubernetes major and minor version (e.g. `release-1.24`).

1. The Travis CI configuration for this repo includes a cron job that runs
   every day. If a new Kubernetes patch version is available for a release, the
   cron job will automatically open a pull request with the necessary changes
   for that patch.

   - If the update is for a new Kubernetes major or minor version, this cron job
     will have to be enabled in Travis CI. Do the following to enable the job:

     1. Navigate to the Travis CI settings page, and locate the `Cron Jobs` section.
     1. Beneath the list of current cron configurations, create a new
        configuration with these specifications:
        - Branch: `release-<major>.<minor>` (e.g. `release-1.24`)
        - Interval: `Daily`
        - Options: `Always Run`
     1. Click `Add`

   - This task can also be run manually to skip having to wait for the cron job
     to trigger. Running the following steps will create the PR:

     1. `cd vagrant-kube-build`
     1. `./build.sh make kube-update KUBE_VERSION=vX.Y.Z` (e.g `v1.24.1`)
     1. Go to the URL displayed in the build output to create the pull request.

1. go.mod and go.sum dependencies are kept up to date with the
   [renovate](https://docs.renovatebot.com/golang/) application.
   One or more pull requests with the necessary changes will be created
   and must be reviewed and merged.

1. If the current branch is the latest branch, update this repository's branch
   settings to make it the default branch.

1. If the update is for a new Kubernetes major or minor version, update the
   [IBM CCM base image](./cmd/ibm-cloud-controller-manager/Dockerfile)
   if an update is available.

1. If the update is for a new Kubernetes major or minor version, open a PR to
   update the golangci-lint version in `.travis.yml` to use the latest release.
   Available releases can be found [here](https://github.com/golangci/golangci-lint/releases).
   You may need to make changes to the code to achieve compliance with the
   latest linting version.

1. If the update is for a new Kubernetes major or minor version, update the
   nightly build job to include the branch in the releases to tag. Ensure the
   job is rebuilt with the changes before proceeding to the next step.

1. Once all PRs are merged, follow the [release process](#release-process) to build the IBM Cloud Provider for the update.

## Release Process

Travis CI is used to build IBM Cloud Provider releases. A nightly build job will
trigger a Travis build by publishing a new tag when there are changes for a
release.
