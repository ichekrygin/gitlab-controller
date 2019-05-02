/*
Copyright 2019 The GitLab-Controller Authors.

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

package sharedsecrets

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	rt "runtime"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	NameFormat = "%s-shared-secrets"

	generateScriptFileName = "generate.sh"
	generateSecretsKey     = "generate-secrets"

	jobTemplateLabelKeyApp = "app"
	jobContainerImage      = "registry.gitlab.com/gitlab-org/build/cng/kubectl:1f8690f03f7aeef27e727396927ab3cc96ac89e7"
	jobResourceRequestCPU  = "50m"
)

var (
	generateScriptData []byte

	jobCommand      = []string{"/bin/bash", "/scripts/generate-secrets"}
	jobVolumeMounts = []corev1.VolumeMount{
		{
			Name:      "scripts",
			MountPath: "/scripts",
		},
		{
			Name:      "ssh",
			MountPath: "/etc/ssh",
		},
	}
)

func init() {
	_, file, _, _ := rt.Caller(0)
	dir := filepath.Dir(file)

	var err error
	if generateScriptData, err = ioutil.ReadFile(filepath.Join(dir, generateScriptFileName)); err != nil { // nolint:gosec
		panic(err)
	}
}

// ObjectMeta helper returns metadata with provide namespace and name values
func ObjectMeta(ns, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace: ns,
		Name:      name,
	}
}

// ConfigMap for shared-secrets
func ConfigMap(ns, name string) *corev1.ConfigMap {
	return configmap(ns, name, string(generateScriptData))
}

func configmap(ns, name, script string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(ns, name),
		Data: map[string]string{
			generateSecretsKey: script,
		},
	}
}

// Job for shared-secrets
func Job(ns, name string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: ObjectMeta(ns, name),
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						jobTemplateLabelKeyApp: name,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: name,
					RestartPolicy:      corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:         name,
							Image:        jobContainerImage,
							Command:      jobCommand,
							VolumeMounts: jobVolumeMounts,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse(jobResourceRequestCPU),
								},
							},
						},
					},
				},
			},
		},
	}
}

// Role for shared-secrets
func Role(ns, name string) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: ObjectMeta(ns, name),
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list", "create", "patch"},
			},
		},
	}
}

// RoleBinding for shared-secrets
func RoleBinding(ns, name string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: ObjectMeta(ns, name),
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "role",
			Name:     name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      name,
				Namespace: ns,
			},
		},
	}
}

// ServiceAccount for shared-secrets
func ServiceAccount(ns, name string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: ObjectMeta(ns, name),
	}
}

// Objects for shared secrets application
func Objects(ns, prefix string) map[string]runtime.Object {
	name := fmt.Sprintf(NameFormat, prefix)
	return map[string]runtime.Object{
		"configmap":      ConfigMap(ns, name),
		"job":            Job(ns, name),
		"role":           Role(ns, name),
		"rolebinding":    RoleBinding(ns, name),
		"serviceaccount": ServiceAccount(ns, name),
	}
}
