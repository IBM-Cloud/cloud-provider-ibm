module cloud.ibm.com/cloud-provider-ibm

go 1.16

require (
	github.com/emicklei/go-restful v2.9.6+incompatible // indirect
	github.com/go-openapi/jsonreference v0.19.6 // indirect
	github.com/go-openapi/swag v0.19.15 // indirect
	github.com/google/go-cmp v0.5.6 // indirect
	github.com/google/uuid v1.1.5 // indirect
	github.com/imdario/mergo v0.3.12 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	google.golang.org/appengine v1.6.7 // indirect
	gopkg.in/gcfg.v1 v1.2.3
	gopkg.in/warnings.v0 v0.1.2 // indirect
	k8s.io/api v0.22.2
	k8s.io/apimachinery v0.22.2
	k8s.io/client-go v0.22.2
	k8s.io/cloud-provider v0.22.2
	k8s.io/component-base v0.22.2
	k8s.io/klog/v2 v2.9.0
)

replace github.com/coreos/etcd => github.com/coreos/etcd v3.3.25+incompatible

// Use forked version of library with security fix cherry-picked
replace github.com/dgrijalva/jwt-go v3.2.0+incompatible => github.com/form3tech-oss/jwt-go v3.2.1+incompatible
