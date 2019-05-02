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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testName      = "test-name"
	testNamespace = "default"
)

var (
	testMeta = metav1.ObjectMeta{Name: testName, Namespace: testNamespace}
)

func Test_configmap(t *testing.T) {
	type args struct {
		ns     string
		name   string
		script string
	}
	tests := map[string]struct {
		args args
		want *corev1.ConfigMap
	}{
		"default": {
			args: args{
				ns:     testNamespace,
				name:   testName,
				script: "",
			},
			want: &corev1.ConfigMap{
				ObjectMeta: testMeta,
				Data:       map[string]string{generateSecretsKey: ""},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := configmap(tt.args.ns, tt.args.name, tt.args.script)
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("configmap() %s", diff)
			}
		})
	}
}

func TestConfigMap(t *testing.T) {
	got := ConfigMap(testNamespace, testName)
	want := &corev1.ConfigMap{
		ObjectMeta: testMeta,
		Data: map[string]string{
			generateSecretsKey: string(generateScriptData),
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("ConfigMap() %s", diff)
	}
}

func TestJob(t *testing.T) {
	type args struct {
		ns   string
		name string
	}
	tests := map[string]struct {
		args args
		want *batchv1.Job
	}{
		"Defaults": {
			args: args{name: testName, ns: testNamespace},
			want: &batchv1.Job{
				ObjectMeta: testMeta,
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{jobTemplateLabelKeyApp: testName},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:         testName,
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
							RestartPolicy:      corev1.RestartPolicyNever,
							ServiceAccountName: testName,
						},
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := Job(tt.args.ns, tt.args.name)
			if diff := cmp.Diff(got, tt.want, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("ServiceAccount() %s", diff)
			}
		})
	}
}

func TestRole(t *testing.T) {
	type args struct {
		ns   string
		name string
	}
	tests := map[string]struct {
		args args
		want *rbacv1.Role
	}{
		"Defaults": {
			args: args{name: testName, ns: testNamespace},
			want: &rbacv1.Role{
				ObjectMeta: testMeta,
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"secrets"},
						Verbs:     []string{"get", "list", "create", "patch"},
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := Role(tt.args.ns, tt.args.name)
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("ServiceAccount() %s", diff)
			}
		})
	}
}

func TestRoleBinding(t *testing.T) {
	type args struct {
		ns   string
		name string
	}
	tests := map[string]struct {
		args args
		want *rbacv1.RoleBinding
	}{
		"Defaults": {
			args: args{name: testName, ns: testNamespace},
			want: &rbacv1.RoleBinding{
				ObjectMeta: testMeta,
				Subjects: []rbacv1.Subject{
					{
						Kind:      "ServiceAccount",
						Namespace: testNamespace,
						Name:      testName,
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "role",
					Name:     testName,
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := RoleBinding(tt.args.ns, tt.args.name)
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("ServiceAccount() %s", diff)
			}
		})
	}
}

func TestServiceAccount(t *testing.T) {
	type args struct {
		ns   string
		name string
	}
	tests := map[string]struct {
		args args
		want *corev1.ServiceAccount
	}{
		"Defaults": {args: args{}, want: &corev1.ServiceAccount{}},
		"NameOnly": {args: args{name: testName}, want: &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: testName}}},
		"NameAndNamespace": {
			args: args{name: testName, ns: testNamespace},
			want: &corev1.ServiceAccount{ObjectMeta: testMeta},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := ServiceAccount(tt.args.ns, tt.args.name)
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("ServiceAccount() = %v, want %v\n%s", got, tt.want, diff)
			}
		})
	}
}
