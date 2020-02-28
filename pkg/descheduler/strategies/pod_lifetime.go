/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package strategies

import (
	"strings"

	v1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	"sigs.k8s.io/descheduler/cmd/descheduler/app/options"
	"sigs.k8s.io/descheduler/pkg/api"
	"sigs.k8s.io/descheduler/pkg/descheduler/evictions"
	podutil "sigs.k8s.io/descheduler/pkg/descheduler/pod"
	"sigs.k8s.io/descheduler/pkg/utils"
)

//type creator string
type OldPodsMap map[string][]*v1.Pod

// PodLifeTime removes the duplicate pods on node. This strategy evicts all duplicate pods on node.
// A pod is said to be a duplicate of other if both of them are from same creator, kind and are within the same
// namespace. As of now, this strategy won't evict daemonsets, mirror pods, critical pods and pods with local storages.
func PodLifeTime(ds *options.DeschedulerServer, strategy api.DeschedulerStrategy, policyGroupVersion string, nodes []*v1.Node, nodepodCount utils.NodePodEvictedCount) {
	if !strategy.Enabled {
		return
	}
	deleteOldPods(ds.Client, policyGroupVersion, nodes, ds.DryRun, nodepodCount, ds.MaxNoOfPodsToEvictPerNode, ds.EvictLocalStoragePods)
}

// deleteOldPods evicts the pod from node and returns the count of evicted pods.
func deleteOldPods(client clientset.Interface, policyGroupVersion string, nodes []*v1.Node, dryRun bool, nodepodCount utils.NodePodEvictedCount, maxPodsToEvict int, evictLocalStoragePods bool) int {
	podsEvicted := 0
	for _, node := range nodes {
		klog.V(1).Infof("Processing node: %#v", node.Name)
		opm := ListOldPodsOnANode(client, node, evictLocalStoragePods)
		for age, pods := range opm {
			if age > 1 { // TODO fix this to only look at old pods defined in the config
				klog.V(1).Infof("%#v", age)
				for i := 0; i < len(pods); i++ {
					if maxPodsToEvict > 0 && nodepodCount[node]+1 > maxPodsToEvict {
						break
					}
					success, err := evictions.EvictPod(client, pods[i], policyGroupVersion, dryRun)
					if !success {
						klog.Infof("Error when evicting pod: %#v (%#v)", pods[i].Name, err)
					} else {
						nodepodCount[node]++
						klog.V(1).Infof("Evicted pod: %#v (%#v)", pods[i].Name, err)
					}
				}
			}
		}
		podsEvicted += nodepodCount[node]
	}
	return podsEvicted
}

func ListOldPodsOnANode(client clientset.Interface, node *v1.Node, evictLocalStoragePods bool) OldPodsMap {
	pods, err := podutil.ListEvictablePodsOnNode(client, node, evictLocalStoragePods)
	if err != nil {
		return nil
	}
	return FindOldPods(pods)
}

// FindOldPods takes a list of pods and returns an OldPodsMap.
func FindOldPods(pods []*v1.Pod) OldPodsMap {
	dpm := OldPodsMap{}
	// Ignoring the error here as in the ListOldPodsOnNode function we call ListEvictablePodsOnNode which checks for error.
	for _, pod := range pods {
		ownerRefList := podutil.OwnerRef(pod)
		for _, ownerRef := range ownerRefList {
			// Namespace/Kind/Name should be unique for the cluster.
			s := strings.Join([]string{pod.ObjectMeta.Namespace, ownerRef.Kind, ownerRef.Name}, "/")
			dpm[s] = append(dpm[s], pod)
		}
	}
	return dpm
}
