package cf

import (
	"github.com/Apicurio/apicurio-registry-operator/controllers/common"
	"github.com/Apicurio/apicurio-registry-operator/controllers/loop"
	"github.com/Apicurio/apicurio-registry-operator/controllers/loop/context"
	"github.com/Apicurio/apicurio-registry-operator/controllers/loop/services"
	"github.com/Apicurio/apicurio-registry-operator/controllers/svc/client"
	"github.com/Apicurio/apicurio-registry-operator/controllers/svc/factory"
	"github.com/Apicurio/apicurio-registry-operator/controllers/svc/resources"
	"github.com/Apicurio/apicurio-registry-operator/controllers/svc/status"
	core "k8s.io/api/core/v1"
	api_errors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ loop.ControlFunction = &ServiceAccountCF{}

type ServiceAccountCF struct {
	ctx                *context.LoopContext
	svcResourceCache   resources.ResourceCache
	svcClients         *client.Clients
	svcStatus          *status.Status
	svcKubeFactory     *factory.KubeFactory
	serviceAccounts    []core.ServiceAccount
	isCached           bool
	serviceAccountName string
	serviceName        string
}

func NewServiceAccountCF(ctx *context.LoopContext, services *services.LoopServices) loop.ControlFunction {

	return &ServiceAccountCF{
		ctx:                ctx,
		svcResourceCache:   ctx.GetResourceCache(),
		svcClients:         services.GetClients(),
		svcStatus:          services.GetStatus(),
		svcKubeFactory:     services.GetKubeFactory(),
		isCached:           false,
		serviceAccounts:    make([]core.ServiceAccount, 0),
		serviceAccountName: resources.RC_EMPTY_NAME,
		serviceName:        resources.RC_EMPTY_NAME,
	}
}

func (this *ServiceAccountCF) Describe() string {
	return "ServiceAccountCF"
}

func (this *ServiceAccountCF) Sense() {

	// Observation #1
	// Get cached Ingress
	serviceAccountEntry, serviceAccountExists := this.svcResourceCache.Get(resources.RC_KEY_SERVICE_ACCOUNT)
	if serviceAccountExists {
		this.serviceAccountName = serviceAccountEntry.GetName().Str()
	} else {
		this.serviceAccountName = resources.RC_EMPTY_NAME
	}
	this.isCached = serviceAccountExists

	// Observation #2
	// Get ingress(s) we *should* track
	this.serviceAccounts = make([]core.ServiceAccount, 0)
	serviceAccounts, err := this.svcClients.Kube().GetServiceAccounts(
		this.ctx.GetAppNamespace(),
		&meta.ListOptions{
			LabelSelector: "app=" + this.ctx.GetAppName().Str(),
		})
	if err == nil {
		for _, serviceAccount := range serviceAccounts.Items {
			if serviceAccount.GetObjectMeta().GetDeletionTimestamp() == nil {
				this.serviceAccounts = append(this.serviceAccounts, serviceAccount)
			}
		}
	}

	// Update the status
	this.svcStatus.SetConfig(status.CFG_STA_SERVICE_ACCOUNT_NAME, this.serviceAccountName)
}

func (this *ServiceAccountCF) Compare() bool {
	// Condition #1
	// If we already have a ingress cached, skip
	return !this.isCached
}

func (this *ServiceAccountCF) Respond() {
	// Response #1
	// We already know about a ingress (name), and it is in the list
	if this.serviceAccountName != resources.RC_EMPTY_NAME {
		contains := false
		for _, val := range this.serviceAccounts {
			if val.Name == this.serviceAccountName {
				contains = true
				this.svcResourceCache.Set(resources.RC_KEY_SERVICE_ACCOUNT, resources.NewResourceCacheEntry(common.Name(val.Name), &val))
				break
			}
		}
		if !contains {
			this.serviceAccountName = resources.RC_EMPTY_NAME
		}
	}
	// Response #2
	// Can follow #1, but there must be a single ingress available
	if this.serviceAccountName == resources.RC_EMPTY_NAME && len(this.serviceAccounts) == 1 {
		serviceAccount := this.serviceAccounts[0]
		this.serviceAccountName = serviceAccount.Name
		this.svcResourceCache.Set(resources.RC_KEY_SERVICE_ACCOUNT, resources.NewResourceCacheEntry(common.Name(serviceAccount.Name), &serviceAccount))
	}
	// Response #3 (and #4)
	// If there is no ingress available (or there are more than 1), just create a new one
	if this.serviceAccountName == resources.RC_EMPTY_NAME && len(this.serviceAccounts) != 1 {
		serviceAccount := this.svcKubeFactory.CreateServiceAccount()
		// leave the creation itself to patcher+creator so other CFs can update
		this.svcResourceCache.Set(resources.RC_KEY_SERVICE_ACCOUNT, resources.NewResourceCacheEntry(resources.RC_EMPTY_NAME, serviceAccount))
	}
}

func (this *ServiceAccountCF) Cleanup() bool {
	// Service Account should not have any deletion dependencies
	if serviceAccountEntry, serviceAccountExists := this.svcResourceCache.Get(resources.RC_KEY_SERVICE_ACCOUNT); serviceAccountExists {
		if err := this.svcClients.Kube().DeleteServiceAccount(serviceAccountEntry.GetValue().(*core.ServiceAccount), &meta.DeleteOptions{}); err != nil && !api_errors.IsNotFound(err) {
			this.ctx.GetLog().Error(err, "Could not delete service account during cleanup.")
			return false
		} else {
			this.svcResourceCache.Remove(resources.RC_KEY_SERVICE_ACCOUNT)
			this.ctx.GetLog().Info("Service account has been deleted.")
		}
	}
	return true
}
