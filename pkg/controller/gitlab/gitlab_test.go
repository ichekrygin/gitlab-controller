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

package gitlab

import (
	"context"
	"testing"

	xpcorev1alpha1 "github.com/crossplaneio/crossplane/pkg/apis/core/v1alpha1"
	xpworkloadv1alpha1 "github.com/crossplaneio/crossplane/pkg/apis/workload/v1alpha1"
	"github.com/go-test/deep"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplaneio/gitlab-controller/pkg/apis/controller"
	"github.com/crossplaneio/gitlab-controller/pkg/apis/controller/v1alpha1"
	"github.com/crossplaneio/gitlab-controller/pkg/test"
)

const (
	testNamespace = "default"
	testName      = "test-gitlab"
)

var (
	testKey = types.NamespacedName{
		Namespace: testNamespace,
		Name:      testName,
	}

	testRequest = reconcile.Request{NamespacedName: testKey}
)

func init() {
	if err := controller.AddToScheme(scheme.Scheme); err != nil {
		panic(err)
	}
}

func TestReconciler_Reconcile(t *testing.T) {
	testError := errors.New("test-error")
	type want struct {
		res reconcile.Result
		err error
	}
	tests := []struct {
		name    string
		client  client.Client
		request reconcile.Request
		want    want
	}{
		{
			name:    "ErrorRetrieving NotFound",
			client:  fake.NewFakeClient(),
			request: testRequest,
			want:    want{res: reconcile.Result{}},
		},
		{
			name: "ErrorRetrieving Other",
			client: &test.MockClient{
				MockGet: func(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
					return testError
				},
			},
			request: testRequest,
			want:    want{res: reconcile.Result{}, err: testError},
		},
		{
			name: "Success",
			client: &test.MockClient{
				MockGet: func(ctx context.Context, key client.ObjectKey, obj runtime.Object) error { return nil },
				MockStatusUpdate: func(ctx context.Context, obj runtime.Object) error {
					g, ok := obj.(*v1alpha1.GitLab)
					if !ok {
						t.Errorf("Reconciler.Reconcile() unexpected type = %T, want %T", obj, &v1alpha1.GitLab{})
					}
					if !g.Status.IsReady() {
						t.Errorf("Reconciler.Reconcile() invalid status = %v, wantErr %v", g.Status.State,
							xpcorev1alpha1.Ready)
					}
					return nil
				},
			},
			request: testRequest,
			want:    want{res: reconcile.Result{RequeueAfter: requeueAfterSuccess}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Reconciler{
				Client: tt.client,
			}
			got, err := r.Reconcile(tt.request)
			if diff := deep.Equal(err, tt.want.err); diff != nil {
				t.Errorf("Reconciler.Reconcile() error = %v, wantErr %v", err, tt.want.err)
			}
			if diff := deep.Equal(got, tt.want.res); diff != nil {
				t.Errorf("Reconciler.Reconcile() = %v, want %v\n%s", got, tt.want, diff)
			}
		})
	}
}

func Test_convert(t *testing.T) {
	type want struct {
		obj *unstructured.Unstructured
		err error
	}
	tests := map[string]struct {
		args runtime.Object
		want want
	}{
		"EmptyConfigmap": {
			args: &corev1.ConfigMap{},
			want: want{
				obj: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"creationTimestamp": nil,
						},
					},
				},
			},
		},
		"Secret Service": {
			args: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      testName,
				},
				Data: map[string][]byte{
					"foo": []byte("bar"),
				},
			},
			want: want{
				obj: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"GetNamespace":      testNamespace,
							"GetName":           testName,
							"creationTimestamp": nil,
						},
						"data": map[string]interface{}{
							"foo": "YmFy", // YmFy is base64 encoded "bar"
						},
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := convert(tt.args)
			if diff := deep.Equal(err, tt.want.err); diff != nil {
				t.Errorf("convert() error = %v, wantErr %v\n%s", err, tt.want.err, diff)
				return
			}
			if diff := cmp.Diff(got, tt.want.obj); diff != "" {
				t.Errorf("convert() = %v, want %v\n%s", got, tt.want.obj, diff)
			}
		})
	}
}

func TestSharedSecrets(t *testing.T) {
	type args struct {
		ns   string
		name string
	}
	type want struct {
		app *xpworkloadv1alpha1.KubernetesApplication
		err error
	}
	tests := map[string]struct {
		args args
		want want
	}{
		"default": {
			args: args{ns: testNamespace, name: testName},
			want: want{app: &xpworkloadv1alpha1.KubernetesApplication{}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := SharedSecretsApp(tt.args.ns, tt.args.name)
			if diff := cmp.Diff(err, tt.want.err); diff != "" {
				t.Errorf("SharedSecrets() error %s", diff)
			}
			if diff := cmp.Diff(got, tt.want.app); diff != "" {
				t.Errorf("SharedSecrets() %s", diff)
			}
		})
	}
}
