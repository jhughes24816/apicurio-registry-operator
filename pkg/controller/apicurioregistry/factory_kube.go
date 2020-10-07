package apicurioregistry

import (
	ar "github.com/Apicurio/apicurio-registry-operator/pkg/apis/apicur/v1alpha1"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	policy "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"os"
)

type KubeFactory struct {
	ctx *Context
}

func NewKubeFactory(ctx *Context) *KubeFactory {
	return &KubeFactory{
		ctx: ctx,
	}
}

const ENV_REGISTRY_VERSION = "REGISTRY_VERSION"
const ENV_OPERATOR_NAME = "OPERATOR_NAME"

// MUST NOT be used directly as selector labels, because some of them may change.
func (this *KubeFactory) GetLabels() map[string]string {

	registryVersion := os.Getenv(ENV_REGISTRY_VERSION)
	if registryVersion == "" {
		panic("Could not determine registry version. Environment variable '" + ENV_REGISTRY_VERSION + "' is empty.")
	}
	operatorName := os.Getenv(ENV_OPERATOR_NAME)
	if operatorName == "" {
		panic("Could not determine operator name. Environment variable '" + ENV_OPERATOR_NAME + "' is empty.")
	}
	app := this.ctx.GetConfiguration().GetAppName()

	return map[string]string{
		"app": app,

		"apicur.io/type":    "apicurio-registry",
		"apicur.io/name":    app,
		"apicur.io/version": registryVersion,

		"app.kubernetes.io/name":     "apicurio-registry",
		"app.kubernetes.io/instance": app,
		"app.kubernetes.io/version":  registryVersion,

		"app.kubernetes.io/managed-by": operatorName,
	}
}

// Selector labels MUST be static/constant in the life of the application.
// Labels that can change during operator/SCV upgrade, such as "apicur.io/version" MUST NOT be used.
func (this *KubeFactory) GetSelectorLabels() map[string]string {
	return map[string]string{
		"app": this.ctx.GetConfiguration().GetAppName(),
	}
}

func (this *KubeFactory) createObjectMeta(typeTag string) meta.ObjectMeta {
	return meta.ObjectMeta{
		GenerateName: this.ctx.GetConfiguration().GetAppName() + "-" + typeTag + "-",
		Namespace:    this.ctx.GetConfiguration().GetAppNamespace(),
		Labels:       this.GetLabels(),
	}
}

func (this *KubeFactory) CreateDeployment() *apps.Deployment {
	var terminationGracePeriodSeconds int64 = 30
	var replicas int32 = 1

	return &apps.Deployment{
		ObjectMeta: this.createObjectMeta("deployment"),
		Spec: apps.DeploymentSpec{
			Replicas: &replicas,
			Selector: &meta.LabelSelector{MatchLabels: this.GetSelectorLabels()},
			Template: core.PodTemplateSpec{
				ObjectMeta: meta.ObjectMeta{
					Labels: this.GetLabels(),
				},
				Spec: core.PodSpec{
					Containers: []core.Container{{
						Name:  this.ctx.GetConfiguration().GetAppName(),
						Image: "",
						Ports: []core.ContainerPort{
							{
								ContainerPort: 8080,
								Protocol:      "TCP",
							},
						},
						Env: []core.EnvVar{},
						Resources: core.ResourceRequirements{
							Limits: core.ResourceList{
								core.ResourceCPU:    resource.MustParse("1"),
								core.ResourceMemory: resource.MustParse("1300Mi"),
							},
							Requests: core.ResourceList{
								core.ResourceCPU:    resource.MustParse("0.1"),
								core.ResourceMemory: resource.MustParse("600Mi"),
							},
						},
						LivenessProbe: &core.Probe{
							Handler: core.Handler{
								HTTPGet: &core.HTTPGetAction{
									Path: "/health/live",
									Port: intstr.FromInt(8080),
								},
							},
							InitialDelaySeconds: 5,
							TimeoutSeconds:      5,
							PeriodSeconds:       10,
							SuccessThreshold:    1,
							FailureThreshold:    3,
						},
						ReadinessProbe: &core.Probe{
							Handler: core.Handler{
								HTTPGet: &core.HTTPGetAction{
									Path: "/health/ready",
									Port: intstr.FromInt(8080),
								},
							},
							InitialDelaySeconds: 5,
							TimeoutSeconds:      5,
							PeriodSeconds:       10,
							SuccessThreshold:    1,
							FailureThreshold:    3,
						},
						TerminationMessagePath: "/dev/termination-log",
						ImagePullPolicy:        core.PullAlways,
					}},
					RestartPolicy:                 core.RestartPolicyAlways,
					TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
					DNSPolicy:                     core.DNSClusterFirst,
				},
			},
			Strategy: apps.DeploymentStrategy{
				Type: apps.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &apps.RollingUpdateDeployment{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
					MaxSurge:       &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
				},
			},
		},
	}
}

func (this *KubeFactory) CreateService() *core.Service {
	service := &core.Service{
		ObjectMeta: this.createObjectMeta("service"),
		Spec: core.ServiceSpec{
			Ports: []core.ServicePort{
				{
					Protocol:   core.ProtocolTCP,
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
			Selector:        this.GetSelectorLabels(),
			Type:            core.ServiceTypeClusterIP,
			SessionAffinity: core.ServiceAffinityNone,
		},
	}
	return service
}

func (this *KubeFactory) CreateIngress(serviceName string) *v1beta1.Ingress {
	if serviceName == "" {
		panic("Required argument.")
	}
	metaData := this.createObjectMeta("ingress")
	metaData.Annotations = map[string]string{
		"nginx.ingress.kubernetes.io/force-ssl-redirect": "false",
		"nginx.ingress.kubernetes.io/rewrite-target":     "/",
		"nginx.ingress.kubernetes.io/ssl-redirect":       "false",
	}
	res := &v1beta1.Ingress{
		ObjectMeta: metaData,
		Spec: v1beta1.IngressSpec{
			Rules: []v1beta1.IngressRule{
				{
					Host: "",
					IngressRuleValue: v1beta1.IngressRuleValue{
						HTTP: &v1beta1.HTTPIngressRuleValue{
							Paths: []v1beta1.HTTPIngressPath{
								{
									Path: "/",
									Backend: v1beta1.IngressBackend{
										ServiceName: serviceName,
										ServicePort: intstr.FromInt(8080),
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return res
}

func (this *KubeFactory) CreateStatus(spec *ar.ApicurioRegistry) *ar.ApicurioRegistryStatus {
	res := &ar.ApicurioRegistryStatus{
		Image:          this.ctx.GetConfiguration().GetConfig(CFG_STA_IMAGE),
		DeploymentName: this.ctx.GetConfiguration().GetConfig(CFG_STA_DEPLOYMENT_NAME),
		ServiceName:    this.ctx.GetConfiguration().GetConfig(CFG_STA_SERVICE_NAME),
		IngressName:    this.ctx.GetConfiguration().GetConfig(CFG_STA_INGRESS_NAME),
		ReplicaCount:   *this.ctx.GetConfiguration().GetConfigInt32P(CFG_STA_REPLICA_COUNT),
		Host:           this.ctx.GetConfiguration().GetConfig(CFG_STA_ROUTE),
	}
	return res
}

func (this *KubeFactory) CreatePodDisruptionBudget() *policy.PodDisruptionBudget {
	podDisruptionBudget := &policy.PodDisruptionBudget{
		ObjectMeta: this.createObjectMeta("pdb"),
		Spec: policy.PodDisruptionBudgetSpec{
			Selector: &meta.LabelSelector{
				MatchLabels: this.GetSelectorLabels(),
			},
			MaxUnavailable: &intstr.IntOrString{
				IntVal: 1,
			},
		},
	}
	return podDisruptionBudget
}
