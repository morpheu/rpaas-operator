// Copyright 2019 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpaas

import (
	"context"
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	nginxv1alpha1 "github.com/tsuru/nginx-operator/pkg/apis/nginx/v1alpha1"
	"github.com/tsuru/rpaas-operator/config"
	nginxManager "github.com/tsuru/rpaas-operator/internal/pkg/rpaas/nginx"
	"github.com/tsuru/rpaas-operator/pkg/apis/extensions/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

type fakeCacheManager struct {
	purgeCacheFunc func(host, path string, preservePath bool) error
}

func (f fakeCacheManager) PurgeCache(host, path string, preservePath bool) error {
	if f.purgeCacheFunc != nil {
		return f.purgeCacheFunc(host, path, preservePath)
	}
	return nil
}

func init() {
	logf.SetLogger(logf.ZapLogger(true))
}

func Test_k8sRpaasManager_DeleteBlock(t *testing.T) {
	tests := []struct {
		name      string
		instance  string
		block     string
		resources func() []runtime.Object
		assertion func(*testing.T, error, v1alpha1.RpaasInstance)
	}{
		{
			name:     "when block does not exist",
			instance: "my-instance",
			block:    "unknown-block",
			resources: func() []runtime.Object {
				return []runtime.Object{newEmptyRpaasInstance()}
			},
			assertion: func(t *testing.T, err error, _ v1alpha1.RpaasInstance) {
				assert.Error(t, err)
				assert.Equal(t, NotFoundError{Msg: "block \"unknown-block\" not found"}, err)
			},
		},
		{
			name:     "when removing the last remaining block",
			instance: "another-instance",
			block:    "http",
			resources: func() []runtime.Object {
				instance := newEmptyRpaasInstance()
				instance.Name = "another-instance"
				instance.Spec.Blocks = map[v1alpha1.BlockType]v1alpha1.Value{
					v1alpha1.BlockTypeHTTP: {
						Value: "# Some NGINX configuration at HTTP scope",
					},
				}
				return []runtime.Object{instance}
			},
			assertion: func(t *testing.T, err error, instance v1alpha1.RpaasInstance) {
				assert.NoError(t, err)
				assert.Nil(t, instance.Spec.Blocks)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &k8sRpaasManager{cli: fake.NewFakeClientWithScheme(newScheme(), tt.resources()...)}
			err := manager.DeleteBlock(context.TODO(), tt.instance, tt.block)
			var instance v1alpha1.RpaasInstance
			if err == nil {
				err1 := manager.cli.Get(context.TODO(), types.NamespacedName{
					Name:      tt.instance,
					Namespace: namespaceName(),
				}, &instance)
				require.NoError(t, err1)
			}
			tt.assertion(t, err, instance)
		})
	}
}

func Test_k8sRpaasManager_ListBlocks(t *testing.T) {
	tests := []struct {
		name      string
		resources func() []runtime.Object
		instance  string
		assertion func(t *testing.T, err error, blocks []ConfigurationBlock)
	}{
		{
			name: "when instance not found",
			resources: func() []runtime.Object {
				return []runtime.Object{}
			},
			instance: "unknown-instance",
			assertion: func(t *testing.T, err error, blocks []ConfigurationBlock) {
				assert.Error(t, err)
				assert.True(t, IsNotFoundError(err))
			},
		},
		{
			name: "when instance has no blocks",
			resources: func() []runtime.Object {
				return []runtime.Object{
					newEmptyRpaasInstance(),
				}
			},
			instance: "my-instance",
			assertion: func(t *testing.T, err error, blocks []ConfigurationBlock) {
				assert.NoError(t, err)
				assert.Nil(t, blocks)
			},
		},
		{
			name: "when instance has two blocks from different sources",
			resources: func() []runtime.Object {
				instance := newEmptyRpaasInstance()
				instance.Spec.Blocks = map[v1alpha1.BlockType]v1alpha1.Value{
					v1alpha1.BlockTypeHTTP: {
						Value: "# some NGINX conf at http context",
					},
					v1alpha1.BlockTypeServer: {
						ValueFrom: &v1alpha1.ValueSource{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "my-instance-blocks",
								},
								Key: "server",
							},
						},
					},
				}
				cm := &corev1.ConfigMap{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "ConfigMap",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-instance-blocks",
						Namespace: namespaceName(),
					},
					Data: map[string]string{
						"server": "# some NGINX conf at server context",
					},
				}
				return []runtime.Object{instance, cm}
			},
			instance: "my-instance",
			assertion: func(t *testing.T, err error, blocks []ConfigurationBlock) {
				assert.NoError(t, err)
				assert.Equal(t, []ConfigurationBlock{
					{Name: "http", Content: "# some NGINX conf at http context"},
					{Name: "server", Content: "# some NGINX conf at server context"},
				}, blocks)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &k8sRpaasManager{cli: fake.NewFakeClientWithScheme(newScheme(), tt.resources()...)}
			blocks, err := manager.ListBlocks(context.TODO(), tt.instance)
			tt.assertion(t, err, blocks)
		})
	}
}

func Test_k8sRpaasManager_UpdateBlock(t *testing.T) {
	tests := []struct {
		name      string
		resources func() []runtime.Object
		instance  string
		block     ConfigurationBlock
		assertion func(t *testing.T, err error, instance *v1alpha1.RpaasInstance)
	}{
		{
			name: "when instance is not found",
			resources: func() []runtime.Object {
				return []runtime.Object{}
			},
			instance: "my-instance",
			block:    ConfigurationBlock{Name: "http", Content: "# some NGINX configuration"},
			assertion: func(t *testing.T, err error, _ *v1alpha1.RpaasInstance) {
				assert.Error(t, err)
				assert.True(t, IsNotFoundError(err))
			},
		},
		{
			name: "when block name is not allowed",
			resources: func() []runtime.Object {
				return []runtime.Object{
					newEmptyRpaasInstance(),
				}
			},
			instance: "my-instance",
			block:    ConfigurationBlock{Name: "unknown block"},
			assertion: func(t *testing.T, err error, _ *v1alpha1.RpaasInstance) {
				assert.Error(t, err)
				assert.Equal(t, ValidationError{Msg: "block \"unknown block\" is not allowed"}, err)
			},
		},
		{
			name: "when adding an HTTP block",
			resources: func() []runtime.Object {
				return []runtime.Object{
					newEmptyRpaasInstance(),
				}
			},
			instance: "my-instance",
			block:    ConfigurationBlock{Name: "http", Content: "# my custom http configuration"},
			assertion: func(t *testing.T, err error, instance *v1alpha1.RpaasInstance) {
				require.NoError(t, err)
				assert.Equal(t, map[v1alpha1.BlockType]v1alpha1.Value{
					v1alpha1.BlockTypeHTTP: {
						Value: "# my custom http configuration",
					},
				}, instance.Spec.Blocks)
			},
		},
		{
			name: "when updating an root block",
			resources: func() []runtime.Object {
				instance := newEmptyRpaasInstance()
				instance.Spec.Blocks = map[v1alpha1.BlockType]v1alpha1.Value{
					v1alpha1.BlockTypeRoot: {Value: "# some old root configuration"},
				}
				return []runtime.Object{instance}
			},
			instance: "my-instance",
			block:    ConfigurationBlock{Name: "root", Content: "# my custom http configuration"},
			assertion: func(t *testing.T, err error, instance *v1alpha1.RpaasInstance) {
				require.NoError(t, err)
				assert.Equal(t, map[v1alpha1.BlockType]v1alpha1.Value{
					v1alpha1.BlockTypeRoot: {
						Value: "# my custom http configuration",
					},
				}, instance.Spec.Blocks)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &k8sRpaasManager{
				cli: fake.NewFakeClientWithScheme(newScheme(), tt.resources()...),
			}
			err := manager.UpdateBlock(context.TODO(), tt.instance, tt.block)
			var instance v1alpha1.RpaasInstance
			if err == nil {
				err1 := manager.cli.Get(context.TODO(), types.NamespacedName{Name: tt.instance, Namespace: namespaceName()}, &instance)
				require.NoError(t, err1)
			}
			tt.assertion(t, err, &instance)
		})
	}
}

func Test_k8sRpaasManager_UpdateCertificate(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	v1alpha1.SchemeBuilder.AddToScheme(scheme)

	ecdsaCertPem := `-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIQIRi6zePL6mKjOipn+dNuaTAKBggqhkjOPQQDAjASMRAw
DgYDVQQKEwdBY21lIENvMB4XDTE3MTAyMDE5NDMwNloXDTE4MTAyMDE5NDMwNlow
EjEQMA4GA1UEChMHQWNtZSBDbzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABD0d
7VNhbWvZLWPuj/RtHFjvtJBEwOkhbN/BnnE8rnZR8+sbwnc/KhCk3FhnpHZnQz7B
5aETbbIgmuvewdjvSBSjYzBhMA4GA1UdDwEB/wQEAwICpDATBgNVHSUEDDAKBggr
BgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MCkGA1UdEQQiMCCCDmxvY2FsaG9zdDo1
NDUzgg4xMjcuMC4wLjE6NTQ1MzAKBggqhkjOPQQDAgNIADBFAiEA2zpJEPQyz6/l
Wf86aX6PepsntZv2GYlA5UpabfT2EZICICpJ5h/iI+i341gBmLiAFQOyTDT+/wQc
6MF9+Yw1Yy0t
-----END CERTIFICATE-----
`

	ecdsaKeyPem := `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIIrYSSNQFaA2Hwf1duRSxKtLYX5CB04fSeQ6tF1aY/PuoAoGCCqGSM49
AwEHoUQDQgAEPR3tU2Fta9ktY+6P9G0cWO+0kETA6SFs38GecTyudlHz6xvCdz8q
EKTcWGekdmdDPsHloRNtsiCa697B2O9IFA==
-----END EC PRIVATE KEY-----
`

	ecdsaCertificate, err := tls.X509KeyPair([]byte(ecdsaCertPem), []byte(ecdsaKeyPem))
	require.NoError(t, err)

	rsaCertPem := `-----BEGIN CERTIFICATE-----
MIIB9TCCAV6gAwIBAgIRAIpoagB8BUn8x36iyvafmC0wDQYJKoZIhvcNAQELBQAw
EjEQMA4GA1UEChMHQWNtZSBDbzAeFw0xOTAzMjYyMDIxMzlaFw0yMDAzMjUyMDIx
MzlaMBIxEDAOBgNVBAoTB0FjbWUgQ28wgZ8wDQYJKoZIhvcNAQEBBQADgY0AMIGJ
AoGBAOIsM9LhHqI3oBhHDCGZkGKgiI72ghnLr5UpaA3I9U7np/LPzt/JpWRG4wjF
5Var2IRPGoNwLcdybFW0YTqvw1wNY88q9BcpwS5PeV7uWyZqWafdSxxveaG6VeCH
YFMqopOKri4kJ4sZB9WS3xMlGZXK6zHPwA4xPtuVEND+LI17AgMBAAGjSzBJMA4G
A1UdDwEB/wQEAwIFoDATBgNVHSUEDDAKBggrBgEFBQcDATAMBgNVHRMBAf8EAjAA
MBQGA1UdEQQNMAuCCWxvY2FsaG9zdDANBgkqhkiG9w0BAQsFAAOBgQCaF9zDYoPh
4KmqxFI3KB+cl8Z/0y0txxH4vqlnByBBiCLpPzivcCRFlT1bGPVJOLsyd/BdOset
yTcvMUPbnEPXZMR4Dsbzzjco1JxMSvZgkhm85gAlwNGjFZrMXqO8G5R/gpWN3UUc
7likRQOu7q61DlicQAZXRnOh6BbKaq1clg==
-----END CERTIFICATE-----
`

	rsaKeyPem := `-----BEGIN RSA PRIVATE KEY-----
MIICXQIBAAKBgQDiLDPS4R6iN6AYRwwhmZBioIiO9oIZy6+VKWgNyPVO56fyz87f
yaVkRuMIxeVWq9iETxqDcC3HcmxVtGE6r8NcDWPPKvQXKcEuT3le7lsmalmn3Usc
b3mhulXgh2BTKqKTiq4uJCeLGQfVkt8TJRmVyusxz8AOMT7blRDQ/iyNewIDAQAB
AoGBAI05gJqayyALj8HZCzAnzUpoZxytvAsTbm27TyfcZaCBchNhwxFlvgphYP5n
Y468+xOSuUF9WHiDcDYLzfJxMZAqmuS+D/IREYDkcrGVT1MXfSCkNaFVqG52+hLZ
GmGsy8+KsJnDJ1HYmwfSnaTj3L8+Bf2Hg291Yb1caRH9+5vBAkEA7P5N3cSN73Fa
HwaWzqkaY75mCR4TpRi27YWGA3wdQek2G71HiSbCOxrWOymvgoNRi6M/sdrP5PTt
JAFxC+pd8QJBAPRPvS0Tm/0lMIZ0q7jxyoW/gKDzokmSszopdlvSU53lN06vaYdK
XyTvqOO95nJx0DjkdM26QojJlSueMTitJisCQDuxNfWku0dTGqrz4uo8p5v16gdj
3vjXh8O9vOqFyWy/i9Ri0XDXJVbzxH/0WPObld+BB9sJTRHTKyPFhS7GIlECQDZ8
chxTez6BxMi3zHR6uEgL5Yv/yfnOldoq1RK1XaChNix+QnLBy2ZZbLkd6P8tEtsd
WE9pct0+193ace/J7fECQQDAhwHBpJjhM+k97D92akneKXIUBo+Egr5E5qF9/g5I
sM5FaDCEIJVbWjPDluxUGbVOQlFHsJs+pZv0Anf9DPwU
-----END RSA PRIVATE KEY-----
`

	rsaCertificate, err := tls.X509KeyPair([]byte(rsaCertPem), []byte(rsaKeyPem))
	require.NoError(t, err)

	instance1 := newEmptyRpaasInstance()

	instance2 := newEmptyRpaasInstance()
	instance2.Name = "another-instance"
	instance2.Spec.Certificates = &nginxv1alpha1.TLSSecret{
		SecretName: "another-instance-certificates",
		Items: []nginxv1alpha1.TLSSecretItem{
			{CertificateField: "default.crt", KeyField: "default.key"},
		},
	}

	secret := newEmptySecret()
	secret.Name = "another-instance-certificates"
	secret.Data = map[string][]byte{
		"default.crt": []byte(rsaCertPem),
		"default.key": []byte(rsaKeyPem),
	}

	resources := []runtime.Object{instance1, instance2, secret}

	testCases := []struct {
		name            string
		instanceName    string
		certificateName string
		certificate     tls.Certificate
		assertion       func(*testing.T, error, *k8sRpaasManager)
	}{
		{
			name:         "instance not found",
			instanceName: "instance-not-found",
			certificate:  ecdsaCertificate,
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				assert.Error(t, err)
				assert.True(t, IsNotFoundError(err))
			},
		},
		{
			name:         "adding a new certificate without name, should use default name \"default\"",
			instanceName: "my-instance",
			certificate:  rsaCertificate,
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				require.NoError(t, err)

				instance := v1alpha1.RpaasInstance{}
				err = m.cli.Get(context.Background(), types.NamespacedName{
					Name:      "my-instance",
					Namespace: namespaceName(),
				}, &instance)
				require.NoError(t, err)

				assert.NotNil(t, instance.Spec.Certificates)
				assert.NotEmpty(t, instance.Spec.Certificates.SecretName)

				expectedCertificates := &nginxv1alpha1.TLSSecret{
					SecretName: instance.Spec.Certificates.SecretName,
					Items: []nginxv1alpha1.TLSSecretItem{
						{CertificateField: "default.crt", KeyField: "default.key"},
					},
				}
				assert.Equal(t, expectedCertificates, instance.Spec.Certificates)

				secret := corev1.Secret{}
				err = m.cli.Get(context.Background(), types.NamespacedName{
					Name:      instance.Spec.Certificates.SecretName,
					Namespace: namespaceName(),
				}, &secret)
				require.NoError(t, err)

				expectedSecretData := map[string][]byte{
					"default.crt": []byte(rsaCertPem),
					"default.key": []byte(rsaKeyPem),
				}
				assert.Equal(t, expectedSecretData, secret.Data)
			},
		},
		{
			name:            "adding a new certificate with a custom name",
			instanceName:    "my-instance",
			certificateName: "custom-name",
			certificate:     ecdsaCertificate,
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				require.NoError(t, err)

				instance := v1alpha1.RpaasInstance{}
				err = m.cli.Get(context.Background(), types.NamespacedName{
					Name:      "my-instance",
					Namespace: namespaceName(),
				}, &instance)
				require.NoError(t, err)

				assert.NotNil(t, instance.Spec.Certificates)
				assert.NotEmpty(t, instance.Spec.Certificates.SecretName)

				expectedCertificates := &nginxv1alpha1.TLSSecret{
					SecretName: instance.Spec.Certificates.SecretName,
					Items: []nginxv1alpha1.TLSSecretItem{
						{CertificateField: "custom-name.crt", KeyField: "custom-name.key"},
					},
				}
				assert.Equal(t, expectedCertificates, instance.Spec.Certificates)

				secret := corev1.Secret{}
				err = m.cli.Get(context.Background(), types.NamespacedName{
					Name:      instance.Spec.Certificates.SecretName,
					Namespace: namespaceName(),
				}, &secret)
				require.NoError(t, err)

				expectedSecretData := map[string][]byte{
					"custom-name.crt": []byte(ecdsaCertPem),
					"custom-name.key": []byte(ecdsaKeyPem),
				}
				assert.Equal(t, expectedSecretData, secret.Data)

			},
		},
		{
			name:         "updating an existing certificate from RSA to ECDSA",
			instanceName: "another-instance",
			certificate:  ecdsaCertificate,
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				require.NoError(t, err)

				instance := v1alpha1.RpaasInstance{}
				err = m.cli.Get(context.Background(), types.NamespacedName{
					Name:      "another-instance",
					Namespace: namespaceName(),
				}, &instance)
				require.NoError(t, err)

				assert.NotNil(t, instance.Spec.Certificates)
				assert.NotEmpty(t, instance.Spec.Certificates.SecretName)

				expectedCertificates := &nginxv1alpha1.TLSSecret{
					SecretName: instance.Spec.Certificates.SecretName,
					Items: []nginxv1alpha1.TLSSecretItem{
						{CertificateField: "default.crt", KeyField: "default.key"},
					},
				}
				assert.Equal(t, expectedCertificates, instance.Spec.Certificates)

				secret := corev1.Secret{}
				err = m.cli.Get(context.Background(), types.NamespacedName{
					Name:      instance.Spec.Certificates.SecretName,
					Namespace: namespaceName(),
				}, &secret)
				require.NoError(t, err)

				expectedSecretData := map[string][]byte{
					"default.crt": []byte(ecdsaCertPem),
					"default.key": []byte(ecdsaKeyPem),
				}
				assert.Equal(t, expectedSecretData, secret.Data)
			},
		},
		{
			name:            "adding multiple certificates",
			instanceName:    "another-instance",
			certificateName: "custom-name",
			certificate:     ecdsaCertificate,
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				require.NoError(t, err)

				instance := v1alpha1.RpaasInstance{}
				err = m.cli.Get(context.Background(), types.NamespacedName{
					Name:      "another-instance",
					Namespace: namespaceName(),
				}, &instance)
				require.NoError(t, err)
				assert.NotNil(t, instance.Spec.Certificates)
				assert.NotEmpty(t, instance.Spec.Certificates.SecretName)

				expectedCertificates := &nginxv1alpha1.TLSSecret{
					SecretName: instance.Spec.Certificates.SecretName,
					Items: []nginxv1alpha1.TLSSecretItem{
						{CertificateField: "default.crt", KeyField: "default.key"},
						{CertificateField: "custom-name.crt", KeyField: "custom-name.key"},
					},
				}
				assert.Equal(t, expectedCertificates, instance.Spec.Certificates)

				secret := corev1.Secret{}
				err = m.cli.Get(context.Background(), types.NamespacedName{
					Name:      instance.Spec.Certificates.SecretName,
					Namespace: namespaceName(),
				}, &secret)
				require.NoError(t, err)

				expectedSecretData := map[string][]byte{
					"default.crt":     []byte(rsaCertPem),
					"default.key":     []byte(rsaKeyPem),
					"custom-name.crt": []byte(ecdsaCertPem),
					"custom-name.key": []byte(ecdsaKeyPem),
				}
				assert.Equal(t, expectedSecretData, secret.Data)
			},
		},
		{
			name:         "updating to the same certificate, should do nothing",
			instanceName: "another-instance",
			certificate:  rsaCertificate,
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				assert.Error(t, err)
				assert.Equal(t, &ConflictError{Msg: "certificate \"default\" already is deployed"}, err)
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			manager := &k8sRpaasManager{cli: fake.NewFakeClientWithScheme(scheme, resources...)}
			err := manager.UpdateCertificate(context.Background(), tt.instanceName, tt.certificateName, tt.certificate)
			tt.assertion(t, err, manager)
		})
	}
}

func newEmptyRpaasInstance() *v1alpha1.RpaasInstance {
	return &v1alpha1.RpaasInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "extensions.tsuru.io/v1alpha1",
			Kind:       "RpaasInstance",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-instance",
			Namespace: namespaceName(),
		},
		Spec: v1alpha1.RpaasInstanceSpec{},
	}
}

func newEmptyExtraFiles() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-instance-extra-files",
			Namespace: namespaceName(),
		},
	}
}

func newEmptySecret() *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-secrets",
			Namespace: namespaceName(),
		},
	}
}

func newEmptyLocations() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-locations",
			Namespace: namespaceName(),
		},
	}
}

func Test_k8sRpaasManager_GetInstanceAddress(t *testing.T) {
	testCases := []struct {
		name      string
		resources func() []runtime.Object
		instance  string
		assertion func(*testing.T, string, error)
	}{
		{
			name: "when the Service is LoadBalancer type and already has an external IP, should returns the provided extenal IP",
			resources: func() []runtime.Object {
				instance := newEmptyRpaasInstance()
				return []runtime.Object{
					instance,
					&nginxv1alpha1.Nginx{
						ObjectMeta: metav1.ObjectMeta{
							Name:      instance.Name,
							Namespace: instance.Namespace,
						},
						Status: nginxv1alpha1.NginxStatus{
							Services: []nginxv1alpha1.ServiceStatus{
								{Name: instance.Name + "-service"},
							},
						},
					},
					&corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      instance.Name + "-service",
							Namespace: instance.Namespace,
						},
						Spec: corev1.ServiceSpec{
							Type:      corev1.ServiceTypeLoadBalancer,
							ClusterIP: "10.1.1.9",
						},
						Status: corev1.ServiceStatus{
							LoadBalancer: corev1.LoadBalancerStatus{
								Ingress: []corev1.LoadBalancerIngress{
									{IP: "10.1.2.3"},
								},
							},
						},
					},
				}
			},
			instance: "my-instance",
			assertion: func(t *testing.T, address string, err error) {
				assert.NoError(t, err)
				assert.Equal(t, address, "10.1.2.3")
			},
		},
		{
			name: "when the Service is LoadBalancer type with no external IP provided, should returns an empty address",
			resources: func() []runtime.Object {
				instance := newEmptyRpaasInstance()
				return []runtime.Object{
					instance,
					&nginxv1alpha1.Nginx{
						ObjectMeta: metav1.ObjectMeta{
							Name:      instance.Name,
							Namespace: instance.Namespace,
						},
						Status: nginxv1alpha1.NginxStatus{
							Services: []nginxv1alpha1.ServiceStatus{
								{Name: instance.Name + "-service"},
							},
						},
					},
					&corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      instance.Name + "-service",
							Namespace: instance.Namespace,
						},
						Spec: corev1.ServiceSpec{
							Type:      corev1.ServiceTypeLoadBalancer,
							ClusterIP: "10.1.1.9",
						},
					},
				}
			},
			instance: "my-instance",
			assertion: func(t *testing.T, address string, err error) {
				assert.NoError(t, err)
				assert.Equal(t, address, "")
			},
		},
		{
			name: "when the Service is ClusterIP type, should returns the ClusterIP address",
			resources: func() []runtime.Object {
				instance := newEmptyRpaasInstance()
				instance.Name = "another-instance"
				return []runtime.Object{
					instance,
					&nginxv1alpha1.Nginx{
						ObjectMeta: metav1.ObjectMeta{
							Name:      instance.Name,
							Namespace: instance.Namespace,
						},
						Status: nginxv1alpha1.NginxStatus{
							Services: []nginxv1alpha1.ServiceStatus{
								{Name: instance.Name + "-service"},
							},
						},
					},
					&corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      instance.Name + "-service",
							Namespace: instance.Namespace,
						},
						Spec: corev1.ServiceSpec{
							Type:      corev1.ServiceTypeClusterIP,
							ClusterIP: "10.1.1.9",
						},
					},
				}
			},
			instance: "another-instance",
			assertion: func(t *testing.T, address string, err error) {
				assert.NoError(t, err)
				assert.Equal(t, address, "10.1.1.9")
			},
		},
		{
			name: "when Nginx object has no Services under Status field, should returns an empty address",
			resources: func() []runtime.Object {
				instance := newEmptyRpaasInstance()
				instance.Name = "instance3"
				return []runtime.Object{
					instance,
					&nginxv1alpha1.Nginx{
						ObjectMeta: metav1.ObjectMeta{
							Name:      instance.Name,
							Namespace: instance.Namespace,
						},
						Status: nginxv1alpha1.NginxStatus{},
					},
				}
			},
			instance: "instance3",
			assertion: func(t *testing.T, address string, err error) {
				assert.NoError(t, err)
				assert.Equal(t, address, "")
			},
		},
		{
			name: "when Nginx object is not found, should returns an empty address",
			resources: func() []runtime.Object {
				instance := newEmptyRpaasInstance()
				instance.Name = "instance4"
				return []runtime.Object{
					instance,
				}
			},
			instance: "instance4",
			assertion: func(t *testing.T, address string, err error) {
				assert.NoError(t, err)
				assert.Equal(t, address, "")
			},
		},
		{
			name: "when RpaasInstance is not found, should returns an NotFoundError",
			resources: func() []runtime.Object {
				return []runtime.Object{}
			},
			instance: "not-found-instance",
			assertion: func(t *testing.T, address string, err error) {
				assert.Error(t, err)
				assert.True(t, IsNotFoundError(err))
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			manager := &k8sRpaasManager{
				cli: fake.NewFakeClientWithScheme(newScheme(), tt.resources()...),
			}
			address, err := manager.GetInstanceAddress(context.Background(), tt.instance)
			tt.assertion(t, address, err)
		})
	}
}

func Test_k8sRpaasManager_GetInstanceStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	v1alpha1.SchemeBuilder.AddToScheme(scheme)
	nginxv1alpha1.SchemeBuilder.AddToScheme(scheme)

	instance1 := newEmptyRpaasInstance()
	instance2 := newEmptyRpaasInstance()
	instance2.ObjectMeta.Name = "instance2"
	instance3 := newEmptyRpaasInstance()
	instance3.ObjectMeta.Name = "instance3"
	instance4 := newEmptyRpaasInstance()
	instance4.ObjectMeta.Name = "instance4"
	instance5 := newEmptyRpaasInstance()
	instance5.ObjectMeta.Name = "instance5"
	nginx1 := &nginxv1alpha1.Nginx{
		ObjectMeta: instance1.ObjectMeta,
		Status: nginxv1alpha1.NginxStatus{
			Pods: []nginxv1alpha1.PodStatus{
				{Name: "pod1"},
				{Name: "pod2"},
			},
		},
	}
	nginx2 := &nginxv1alpha1.Nginx{
		ObjectMeta: instance2.ObjectMeta,
		Status: nginxv1alpha1.NginxStatus{
			Pods: []nginxv1alpha1.PodStatus{
				{Name: "pod3"},
			},
		},
	}
	nginx3 := &nginxv1alpha1.Nginx{
		ObjectMeta: instance3.ObjectMeta,
		Status: nginxv1alpha1.NginxStatus{
			Pods: []nginxv1alpha1.PodStatus{},
		},
	}
	nginx4 := &nginxv1alpha1.Nginx{
		ObjectMeta: instance5.ObjectMeta,
		Status: nginxv1alpha1.NginxStatus{
			Pods: []nginxv1alpha1.PodStatus{
				{Name: "pod4"},
			},
		},
	}
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: instance1.Namespace,
		},
		Status: corev1.PodStatus{
			PodIP: "10.0.0.1",
		},
	}
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod2",
			Namespace: instance1.Namespace,
		},
		Status: corev1.PodStatus{
			PodIP: "10.0.0.2",
		},
	}
	pod4 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod4",
			Namespace: instance1.Namespace,
		},
		Status: corev1.PodStatus{
			PodIP: "10.0.0.9",
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Ready: false,
				},
			},
		},
	}
	evt1 := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1.1",
			Namespace: instance1.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Name: "pod1",
			Kind: "Pod",
		},
		Source: corev1.EventSource{
			Component: "c1",
		},
		Message: "msg1",
	}
	evt2 := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1.2",
			Namespace: instance1.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Name: "pod1",
			Kind: "Pod",
		},
		Source: corev1.EventSource{
			Component: "c2",
			Host:      "h1",
		},
		Message: "msg2",
	}

	resources := []runtime.Object{instance1, instance2, instance3, instance4, instance5, nginx1, nginx2, nginx3, nginx4, pod1, pod2, pod4, evt1, evt2}

	testCases := []struct {
		instance  string
		assertion func(*testing.T, PodStatusMap, error)
	}{
		{
			"my-instance",
			func(t *testing.T, podMap PodStatusMap, err error) {
				assert.NoError(t, err)
				assert.Equal(t, podMap, PodStatusMap{
					"pod1": PodStatus{
						Running: true,
						Status:  "msg1 [c1]\nmsg2 [c2, h1]",
						Address: "10.0.0.1",
					},
					"pod2": PodStatus{
						Running: true,
						Status:  "",
						Address: "10.0.0.2",
					},
				})
			},
		},
		{
			"instance2",
			func(t *testing.T, podMap PodStatusMap, err error) {
				assert.NoError(t, err)
				assert.Equal(t, podMap, PodStatusMap{
					"pod3": PodStatus{
						Running: false,
						Status:  "pods \"pod3\" not found",
					},
				})
			},
		},
		{
			"instance3",
			func(t *testing.T, podMap PodStatusMap, err error) {
				assert.NoError(t, err)
				assert.Equal(t, podMap, PodStatusMap{})
			},
		},
		{
			"instance4",
			func(t *testing.T, podMap PodStatusMap, err error) {
				assert.Error(t, err)
				assert.True(t, IsNotFoundError(err))
			},
		},
		{
			"instance5",
			func(t *testing.T, podMap PodStatusMap, err error) {
				assert.NoError(t, err)
				assert.Equal(t, podMap, PodStatusMap{
					"pod4": PodStatus{
						Running: false,
						Address: "10.0.0.9",
					},
				})
			},
		},
		{
			"not-found-instance",
			func(t *testing.T, podMap PodStatusMap, err error) {
				assert.Error(t, err)
				assert.True(t, IsNotFoundError(err))
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.instance, func(t *testing.T) {
			fakeCli := fake.NewFakeClientWithScheme(scheme, resources...)
			manager := &k8sRpaasManager{
				nonCachedCli: fakeCli,
				cli:          fakeCli,
			}
			podMap, err := manager.GetInstanceStatus(context.Background(), testCase.instance)
			testCase.assertion(t, podMap, err)
		})
	}
}

func Test_k8sRpaasManager_CreateExtraFiles(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	v1alpha1.SchemeBuilder.AddToScheme(scheme)
	nginxv1alpha1.SchemeBuilder.AddToScheme(scheme)

	instance1 := newEmptyRpaasInstance()
	instance2 := newEmptyRpaasInstance()
	instance2.Name = "another-instance"
	instance2.Spec.ExtraFiles = &nginxv1alpha1.FilesRef{
		Name: "another-instance-extra-files",
		Files: map[string]string{
			"index.html": "index.html",
		},
	}

	configMap := newEmptyExtraFiles()
	configMap.Name = "another-instance-extra-files"
	configMap.BinaryData = map[string][]byte{
		"index.html": []byte("Hello world"),
	}

	resources := []runtime.Object{instance1, instance2, configMap}

	testCases := []struct {
		instance  string
		files     []File
		assertion func(*testing.T, error, *k8sRpaasManager)
	}{
		{
			instance: "my-instance",
			files: []File{
				{
					Name:    "/path/to/my/file",
					Content: []byte("My invalid filename"),
				},
			},
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				assert.Error(t, err)
				assert.True(t, IsValidationError(err))
			},
		},
		{
			instance: "my-instance",
			files: []File{
				{
					Name:    "www/index.html",
					Content: []byte("<h1>Hello world!</h1>"),
				},
				{
					Name:    "waf/sqli-rules.cnf",
					Content: []byte("# my awesome rules against SQLi :)..."),
				},
			},
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				assert.NoError(t, err)

				instance := v1alpha1.RpaasInstance{}
				err = m.cli.Get(context.Background(), types.NamespacedName{Name: "my-instance", Namespace: namespaceName()}, &instance)
				require.NoError(t, err)

				expectedFiles := map[string]string{
					"www_index.html":     "www/index.html",
					"waf_sqli-rules.cnf": "waf/sqli-rules.cnf",
				}
				assert.Equal(t, expectedFiles, instance.Spec.ExtraFiles.Files)

				cm, err := m.getExtraFiles(context.Background(), instance)
				assert.NoError(t, err)
				expectedConfigMapData := map[string][]byte{
					"www_index.html":     []byte("<h1>Hello world!</h1>"),
					"waf_sqli-rules.cnf": []byte("# my awesome rules against SQLi :)..."),
				}
				assert.Equal(t, expectedConfigMapData, cm.BinaryData)
			},
		},
		{
			instance: "another-instance",
			files: []File{
				{
					Name:    "index.html",
					Content: []byte("My new hello world"),
				},
			},
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				assert.Error(t, err)
				assert.True(t, IsConflictError(err))
				assert.Equal(t, &ConflictError{Msg: `file "index.html" already exists`}, err)
			},
		},
		{
			instance: "another-instance",
			files: []File{
				{
					Name:    "www/index.html",
					Content: []byte("<h1>Hello world!</h1>"),
				},
			},
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				assert.NoError(t, err)

				instance := v1alpha1.RpaasInstance{}
				err = m.cli.Get(context.Background(), types.NamespacedName{Name: "another-instance", Namespace: namespaceName()}, &instance)
				require.NoError(t, err)

				assert.NotEqual(t, "another-instance-extra-files", instance.Spec.ExtraFiles.Name)
				expectedFiles := map[string]string{
					"index.html":     "index.html",
					"www_index.html": "www/index.html",
				}
				assert.Equal(t, expectedFiles, instance.Spec.ExtraFiles.Files)

				cm, err := m.getExtraFiles(context.Background(), instance)
				require.NoError(t, err)

				expectedConfigMapData := map[string][]byte{
					"index.html":     []byte("Hello world"),
					"www_index.html": []byte("<h1>Hello world!</h1>"),
				}
				assert.Equal(t, expectedConfigMapData, cm.BinaryData)
			},
		},
	}

	for _, tt := range testCases {
		t.Run("", func(t *testing.T) {
			manager := &k8sRpaasManager{cli: fake.NewFakeClientWithScheme(scheme, resources...)}
			err := manager.CreateExtraFiles(context.Background(), tt.instance, tt.files...)
			tt.assertion(t, err, manager)
		})
	}
}

func Test_k8sRpaasManager_GetExtraFiles(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	v1alpha1.SchemeBuilder.AddToScheme(scheme)
	nginxv1alpha1.SchemeBuilder.AddToScheme(scheme)

	instance1 := newEmptyRpaasInstance()

	instance2 := newEmptyRpaasInstance()
	instance2.Name = "another-instance"
	instance2.Spec.ExtraFiles = &nginxv1alpha1.FilesRef{
		Name: "another-instance-extra-files",
		Files: map[string]string{
			"index.html": "index.html",
		},
	}

	configMap := newEmptyExtraFiles()
	configMap.Name = "another-instance-extra-files"
	configMap.BinaryData = map[string][]byte{
		"index.html": []byte("Hello world"),
	}

	resources := []runtime.Object{instance1, instance2, configMap}

	testCases := []struct {
		instance      string
		expectedFiles []File
	}{
		{
			instance:      "my-instance",
			expectedFiles: []File{},
		},
		{
			instance: "another-instance",
			expectedFiles: []File{
				{
					Name:    "index.html",
					Content: []byte("Hello world"),
				},
			},
		},
	}

	for _, tt := range testCases {
		t.Run("", func(t *testing.T) {
			manager := &k8sRpaasManager{cli: fake.NewFakeClientWithScheme(scheme, resources...)}
			files, err := manager.GetExtraFiles(context.Background(), tt.instance)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedFiles, files)
		})
	}
}

func Test_k8sRpaasManager_UpdateExtraFiles(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	v1alpha1.SchemeBuilder.AddToScheme(scheme)
	nginxv1alpha1.SchemeBuilder.AddToScheme(scheme)

	instance1 := newEmptyRpaasInstance()

	instance2 := newEmptyRpaasInstance()
	instance2.Name = "another-instance"
	instance2.Spec.ExtraFiles = &nginxv1alpha1.FilesRef{
		Name: "another-instance-extra-files",
		Files: map[string]string{
			"index.html": "index.html",
		},
	}

	configMap := newEmptyExtraFiles()
	configMap.Name = "another-instance-extra-files"
	configMap.BinaryData = map[string][]byte{
		"index.html": []byte("Hello world"),
	}

	resources := []runtime.Object{instance1, instance2, configMap}

	testCases := []struct {
		instance  string
		files     []File
		assertion func(*testing.T, error, *k8sRpaasManager)
	}{
		{
			instance: "my-instance",
			files: []File{
				{
					Name:    "www/index.html",
					Content: []byte("<h1>Hello world!</h1>"),
				},
			},
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				assert.Error(t, err)
				assert.Equal(t, &NotFoundError{Msg: "there are no extra files"}, err)
			},
		},
		{
			instance: "another-instance",
			files: []File{
				{
					Name:    "www/index.html",
					Content: []byte("<h1>Hello world!</h1>"),
				},
			},
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				assert.Error(t, err)
				assert.Equal(t, &NotFoundError{Msg: `file "www/index.html" does not exist`}, err)
			},
		},
		{
			instance: "another-instance",
			files: []File{
				{
					Name:    "index.html",
					Content: []byte("<h1>Hello world!</h1>"),
				},
			},
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				assert.NoError(t, err)

				instance := v1alpha1.RpaasInstance{}
				err = m.cli.Get(context.Background(), types.NamespacedName{Name: "another-instance", Namespace: namespaceName()}, &instance)
				require.NoError(t, err)

				cm, err := m.getExtraFiles(context.Background(), instance)
				require.NoError(t, err)

				expectedConfigMapData := map[string][]byte{
					"index.html": []byte("<h1>Hello world!</h1>"),
				}
				assert.Equal(t, expectedConfigMapData, cm.BinaryData)

			},
		},
	}

	for _, tt := range testCases {
		t.Run("", func(t *testing.T) {
			manager := &k8sRpaasManager{cli: fake.NewFakeClientWithScheme(scheme, resources...)}
			err := manager.UpdateExtraFiles(context.Background(), tt.instance, tt.files...)
			tt.assertion(t, err, manager)
		})
	}
}

func Test_k8sRpaasManager_DeleteExtraFiles(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	v1alpha1.SchemeBuilder.AddToScheme(scheme)
	nginxv1alpha1.SchemeBuilder.AddToScheme(scheme)

	instance1 := newEmptyRpaasInstance()
	instance1.Spec.ExtraFiles = &nginxv1alpha1.FilesRef{
		Name: "my-instance-extra-files",
		Files: map[string]string{
			"index.html":     "index.html",
			"waf_rules.conf": "waf/rules.conf",
		},
	}

	instance2 := newEmptyRpaasInstance()
	instance2.Name = "another-instance"

	configMap := newEmptyExtraFiles()
	configMap.Name = "my-instance-extra-files"
	configMap.BinaryData = map[string][]byte{
		"index.html":     []byte("Hello world"),
		"waf_rules.conf": []byte("# my awesome WAF rules"),
	}

	resources := []runtime.Object{instance1, instance2, configMap}

	testCases := []struct {
		instance  string
		filenames []string
		assertion func(*testing.T, error, *k8sRpaasManager)
	}{
		{
			instance:  "another-instance",
			filenames: []string{"whatever-file.txt"},
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				assert.Error(t, err)
				assert.Equal(t, &NotFoundError{Msg: `there are no extra files`}, err)
			},
		},
		{
			instance:  "my-instance",
			filenames: []string{"index.html", "waf_rules.conf"},
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				assert.NoError(t, err)

				instance := v1alpha1.RpaasInstance{}
				err = m.cli.Get(context.Background(), types.NamespacedName{Name: "my-instance", Namespace: namespaceName()}, &instance)
				require.NoError(t, err)
				assert.Nil(t, instance.Spec.ExtraFiles)
			},
		},
		{
			instance:  "my-instance",
			filenames: []string{"not-found.txt"},
			assertion: func(t *testing.T, err error, m *k8sRpaasManager) {
				assert.Error(t, err)
				assert.Equal(t, &NotFoundError{Msg: `file "not-found.txt" does not exist`}, err)
			},
		},
	}

	for _, tt := range testCases {
		t.Run("", func(t *testing.T) {
			manager := &k8sRpaasManager{cli: fake.NewFakeClientWithScheme(scheme, resources...)}
			err := manager.DeleteExtraFiles(context.Background(), tt.instance, tt.filenames...)
			tt.assertion(t, err, manager)
		})
	}
}
func Test_k8sRpaasManager_PurgeCache(t *testing.T) {
	instance1 := newEmptyRpaasInstance()
	instance1.ObjectMeta.Name = "my-instance"
	instance2 := newEmptyRpaasInstance()
	instance2.ObjectMeta.Name = "not-running-instance"
	nginx1 := &nginxv1alpha1.Nginx{
		ObjectMeta: instance1.ObjectMeta,
		Status: nginxv1alpha1.NginxStatus{
			Pods: []nginxv1alpha1.PodStatus{
				{Name: "my-instance-pod-1"},
				{Name: "my-instance-pod-2"},
			},
		},
	}
	nginx2 := &nginxv1alpha1.Nginx{
		ObjectMeta: instance2.ObjectMeta,
		Status: nginxv1alpha1.NginxStatus{
			Pods: []nginxv1alpha1.PodStatus{
				{Name: "not-running-instance"},
			},
		},
	}
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-instance-pod-1",
			Namespace: instance1.Namespace,
		},
		Status: corev1.PodStatus{
			PodIP:             "10.0.0.9",
			ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
		},
	}
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-instance-pod-2",
			Namespace: instance1.Namespace,
		},
		Status: corev1.PodStatus{
			PodIP:             "10.0.0.10",
			ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
		},
	}
	pod3 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "not-running-instance-pod",
			Namespace: instance2.Namespace,
		},
		Status: corev1.PodStatus{
			PodIP:             "10.0.0.11",
			ContainerStatuses: []corev1.ContainerStatus{{Ready: false}},
		},
	}

	scheme := newScheme()
	resources := []runtime.Object{instance1, instance2, nginx1, nginx2, pod1, pod2, pod3}

	tests := []struct {
		name         string
		instance     string
		args         PurgeCacheArgs
		cacheManager fakeCacheManager
		assertion    func(t *testing.T, count int, err error)
	}{
		{
			name:         "return NotFoundError when instance is not found",
			instance:     "not-found-instance",
			args:         PurgeCacheArgs{Path: "/index.html"},
			cacheManager: fakeCacheManager{},
			assertion: func(t *testing.T, count int, err error) {
				assert.Error(t, err)
				expected := NotFoundError{Msg: "rpaas instance \"not-found-instance\" not found"}
				assert.Equal(t, expected, err)
			},
		},
		{
			name:         "return ValidationError path parameter was not provided",
			instance:     "my-instance",
			args:         PurgeCacheArgs{},
			cacheManager: fakeCacheManager{},
			assertion: func(t *testing.T, count int, err error) {
				assert.Error(t, err)
				expected := ValidationError{Msg: "path is required"}
				assert.Equal(t, expected, err)
			},
		},
		{
			name:         "return 0 when instance doesn't have any running pods",
			instance:     "not-running-instance",
			args:         PurgeCacheArgs{Path: "/index.html"},
			cacheManager: fakeCacheManager{},
			assertion: func(t *testing.T, count int, err error) {
				assert.NoError(t, err)
				assert.Equal(t, 0, count)
			},
		},
		{
			name:         "return the number of nginx instances where cache was purged",
			instance:     "my-instance",
			args:         PurgeCacheArgs{Path: "/index.html"},
			cacheManager: fakeCacheManager{},
			assertion: func(t *testing.T, count int, err error) {
				assert.NoError(t, err)
				assert.Equal(t, 2, count)
			},
		},
		{
			name:     "return the number of nginx instances where cache was purged",
			instance: "my-instance",
			args:     PurgeCacheArgs{Path: "/index.html"},
			cacheManager: fakeCacheManager{
				purgeCacheFunc: func(host, path string, preservePath bool) error {
					if host == "10.0.0.9" {
						return nginxManager.NginxError{Msg: "some nginx error"}
					}
					return nil
				},
			},
			assertion: func(t *testing.T, count int, err error) {
				assert.NoError(t, err)
				assert.Equal(t, 1, count)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeCli := fake.NewFakeClientWithScheme(scheme, resources...)
			manager := &k8sRpaasManager{
				cli:          fakeCli,
				nonCachedCli: fakeCli,
				cacheManager: tt.cacheManager,
			}
			count, err := manager.PurgeCache(context.Background(), tt.instance, tt.args)
			tt.assertion(t, count, err)
		})
	}
}

func Test_k8sRpaasManager_BindApp(t *testing.T) {
	instance1 := newEmptyRpaasInstance()

	instance2 := newEmptyRpaasInstance()
	instance2.Name = "another-instance"
	instance2.Spec.Host = "app2.tsuru.example.com"

	scheme := newScheme()
	resources := []runtime.Object{instance1, instance2}

	tests := []struct {
		name      string
		instance  string
		args      BindAppArgs
		assertion func(t *testing.T, err error, got v1alpha1.RpaasInstance)
	}{
		{
			name:     "when instance not found",
			instance: "not-found-instance",
			assertion: func(t *testing.T, err error, _ v1alpha1.RpaasInstance) {
				assert.Error(t, err)
				expected := NotFoundError{Msg: "rpaas instance \"not-found-instance\" not found"}
				assert.Equal(t, expected, err)
			},
		},
		{
			name:     "when AppHost field is not defined",
			instance: "my-instance",
			args:     BindAppArgs{},
			assertion: func(t *testing.T, err error, _ v1alpha1.RpaasInstance) {
				assert.Error(t, err)
				expected := &ValidationError{Msg: "application host cannot be empty"}
				assert.Equal(t, expected, err)
			},
		},
		{
			name:     "when instance successfully bound with an application",
			instance: "my-instance",
			args: BindAppArgs{
				AppHost: "app1.tsuru.example.com",
			},
			assertion: func(t *testing.T, err error, ri v1alpha1.RpaasInstance) {
				assert.NoError(t, err)
				assert.Equal(t, "app1.tsuru.example.com", ri.Spec.Host)
			},
		},
		{
			name:     "when instance already bound with another application",
			instance: "another-instance",
			args: BindAppArgs{
				AppHost: "app1.tsuru.example.com",
			},
			assertion: func(t *testing.T, err error, _ v1alpha1.RpaasInstance) {
				assert.Error(t, err)
				expected := &ConflictError{Msg: "instance already bound with another application"}
				assert.Equal(t, expected, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &k8sRpaasManager{cli: fake.NewFakeClientWithScheme(scheme, resources...)}
			bindAppErr := manager.BindApp(context.Background(), tt.instance, tt.args)

			var instance v1alpha1.RpaasInstance

			if bindAppErr == nil {
				require.NoError(t, manager.cli.Get(context.Background(), types.NamespacedName{
					Name:      tt.instance,
					Namespace: namespaceName(),
				}, &instance))
			}

			tt.assertion(t, bindAppErr, instance)
		})
	}
}

func Test_k8sRpaasManager_UnbindApp(t *testing.T) {
	instance1 := newEmptyRpaasInstance()

	instance2 := newEmptyRpaasInstance()
	instance2.Name = "another-instance"
	instance2.Spec.Host = "app2.tsuru.example.com"

	scheme := newScheme()
	resources := []runtime.Object{instance1, instance2}

	tests := []struct {
		name      string
		instance  string
		assertion func(t *testing.T, err error, got v1alpha1.RpaasInstance)
	}{
		{
			name:     "when instance not found",
			instance: "not-found-instance",
			assertion: func(t *testing.T, err error, _ v1alpha1.RpaasInstance) {
				assert.Error(t, err)
				expected := NotFoundError{Msg: "rpaas instance \"not-found-instance\" not found"}
				assert.Equal(t, expected, err)
			},
		},
		{
			name:     "when instance bound with no application",
			instance: "my-instance",
			assertion: func(t *testing.T, err error, _ v1alpha1.RpaasInstance) {
				assert.Error(t, err)
				expected := &ValidationError{Msg: "instance not bound"}
				assert.Equal(t, expected, err)
			},
		},
		{
			name:     "when instance successfully unbound",
			instance: "another-instance",
			assertion: func(t *testing.T, err error, ri v1alpha1.RpaasInstance) {
				assert.NoError(t, err)
				assert.Equal(t, "", ri.Spec.Host)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &k8sRpaasManager{cli: fake.NewFakeClientWithScheme(scheme, resources...)}
			unbindAppErr := manager.UnbindApp(context.Background(), tt.instance)

			var instance v1alpha1.RpaasInstance

			if unbindAppErr == nil {
				require.NoError(t, manager.cli.Get(context.Background(), types.NamespacedName{
					Name:      tt.instance,
					Namespace: namespaceName(),
				}, &instance))
			}

			tt.assertion(t, unbindAppErr, instance)
		})
	}
}

func Test_k8sRpaasManager_DeleteRoute(t *testing.T) {
	instance1 := newEmptyRpaasInstance()

	instance2 := newEmptyRpaasInstance()
	instance2.Name = "another-instance"
	instance2.Spec.Locations = []v1alpha1.Location{
		{
			Path: "/path1",
			Content: &v1alpha1.Value{
				Value: "# My NGINX config for /path1 location",
			},
		},
		{
			Path:        "/path2",
			Destination: "app2.tsuru.example.com",
		},
	}

	instance3 := newEmptyRpaasInstance()
	instance3.Name = "new-instance"
	instance3.Spec.Locations = []v1alpha1.Location{
		{
			Path: "/my/custom/path",
			Content: &v1alpha1.Value{
				ValueFrom: &v1alpha1.ValueSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "my-custom-config",
						},
						Key: "just-another-key",
					},
				},
			},
		},
	}

	cm := newEmptyLocations()
	cm.Name = "my-locations-config"
	cm.Data = map[string]string{
		"just-another-key": "# Some NGINX custom conf",
	}

	scheme := newScheme()
	resources := []runtime.Object{instance1, instance2, instance3}

	tests := []struct {
		name      string
		instance  string
		path      string
		assertion func(t *testing.T, err error, ri *v1alpha1.RpaasInstance)
	}{
		{
			name:     "when instance not found",
			instance: "not-found-instance",
			path:     "/path",
			assertion: func(t *testing.T, err error, _ *v1alpha1.RpaasInstance) {
				assert.Error(t, err)
				assert.True(t, IsNotFoundError(err))
			},
		},
		{
			name:     "when locations is nil",
			instance: "my-instance",
			path:     "/path/unknown",
			assertion: func(t *testing.T, err error, _ *v1alpha1.RpaasInstance) {
				assert.Error(t, err)
				assert.True(t, IsNotFoundError(err))
				assert.Equal(t, &NotFoundError{Msg: "path does not exist"}, err)
			},
		},
		{
			name:     "when path does not exist",
			instance: "my-instance",
			path:     "/path/unknown",
			assertion: func(t *testing.T, err error, _ *v1alpha1.RpaasInstance) {
				assert.Error(t, err)
				assert.True(t, IsNotFoundError(err))
				assert.Equal(t, &NotFoundError{Msg: "path does not exist"}, err)
			},
		},
		{
			name:     "when removing a route with destination",
			instance: "another-instance",
			path:     "/path2",
			assertion: func(t *testing.T, err error, ri *v1alpha1.RpaasInstance) {
				assert.NoError(t, err)
				assert.Len(t, ri.Spec.Locations, 1)
				assert.NotEqual(t, "/path2", ri.Spec.Locations[0].Path)
			},
		},
		{
			name:     "when removing a route with custom configuration",
			instance: "another-instance",
			path:     "/path1",
			assertion: func(t *testing.T, err error, ri *v1alpha1.RpaasInstance) {
				assert.NoError(t, err)
				assert.Len(t, ri.Spec.Locations, 1)
				assert.NotEqual(t, "/path1", ri.Spec.Locations[0])
			},
		},
		{
			name:     "when removing the last location",
			instance: "new-instance",
			path:     "/my/custom/path",
			assertion: func(t *testing.T, err error, ri *v1alpha1.RpaasInstance) {
				assert.NoError(t, err)
				assert.Nil(t, ri.Spec.Locations)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &k8sRpaasManager{cli: fake.NewFakeClientWithScheme(scheme, resources...)}
			err := manager.DeleteRoute(context.Background(), tt.instance, tt.path)
			var ri v1alpha1.RpaasInstance
			if err == nil {
				require.NoError(t, manager.cli.Get(context.Background(), types.NamespacedName{Name: tt.instance, Namespace: namespaceName()}, &ri))
			}
			tt.assertion(t, err, &ri)
		})
	}
}

func Test_k8sRpaasManager_GetRoutes(t *testing.T) {
	boolPointer := func(b bool) *bool {
		return &b
	}

	instance1 := newEmptyRpaasInstance()

	instance2 := newEmptyRpaasInstance()
	instance2.Name = "another-instance"
	instance2.Spec.Locations = []v1alpha1.Location{
		{
			Path: "/path1",
			Content: &v1alpha1.Value{
				Value: "# My NGINX config for /path1 location",
			},
		},
		{
			Path:        "/path2",
			Destination: "app2.tsuru.example.com",
		},
		{
			Path:        "/path3",
			Destination: "app3.tsuru.example.com",
			ForceHTTPS:  true,
		},
		{
			Path: "/path4",
			Content: &v1alpha1.Value{
				ValueFrom: &v1alpha1.ValueSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "my-locations",
						},
						Key: "path4",
					},
					Namespace: namespaceName(),
				},
			},
		},
		{
			Path: "/path5",
			Content: &v1alpha1.Value{
				ValueFrom: &v1alpha1.ValueSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "unknown-configmap",
						},
						Key: "path4",
					},
					Namespace: namespaceName(),
				},
			},
		},
		{
			Path: "/path6",
			Content: &v1alpha1.Value{
				ValueFrom: &v1alpha1.ValueSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "my-locations",
						},
						Key: "unknown-key",
					},
					Namespace: namespaceName(),
				},
			},
		},
	}

	instance3 := newEmptyRpaasInstance()
	instance3.Name = "instance3"
	instance3.Spec.Locations = []v1alpha1.Location{
		{
			Path: "/path1",
			Content: &v1alpha1.Value{
				ValueFrom: &v1alpha1.ValueSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "my-locations",
						},
						Key:      "unknown-key",
						Optional: boolPointer(false),
					},
					Namespace: namespaceName(),
				},
			},
		},
	}

	instance4 := newEmptyRpaasInstance()
	instance4.Name = "instance4"
	instance4.Spec.Locations = []v1alpha1.Location{
		{
			Path: "/path1",
			Content: &v1alpha1.Value{
				ValueFrom: &v1alpha1.ValueSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "unknown-configmap",
						},
						Key:      "unknown-key",
						Optional: boolPointer(false),
					},
					Namespace: namespaceName(),
				},
			},
		},
	}

	cm := newEmptyLocations()
	cm.Name = "my-locations"
	cm.Data = map[string]string{
		"path4": "# My NGINX config for /path4 location",
	}

	scheme := newScheme()
	resources := []runtime.Object{instance1, instance2, instance3, instance4, cm}

	tests := []struct {
		name      string
		instance  string
		assertion func(t *testing.T, err error, routes []Route)
	}{
		{
			name:     "when instance not found",
			instance: "not-found-instance",
			assertion: func(t *testing.T, err error, _ []Route) {
				assert.Error(t, err)
				assert.True(t, IsNotFoundError(err))
			},
		},
		{
			name:     "when instance has no custom routes",
			instance: "my-instance",
			assertion: func(t *testing.T, err error, routes []Route) {
				assert.NoError(t, err)
				assert.Len(t, routes, 0)
			},
		},
		{
			name:     "when instance contains multiple locations and with content comes from different sources",
			instance: "another-instance",
			assertion: func(t *testing.T, err error, routes []Route) {
				assert.NoError(t, err)
				assert.Equal(t, []Route{
					{
						Path:    "/path1",
						Content: "# My NGINX config for /path1 location",
					},
					{
						Path:        "/path2",
						Destination: "app2.tsuru.example.com",
					},
					{
						Path:        "/path3",
						Destination: "app3.tsuru.example.com",
						HTTPSOnly:   true,
					},
					{
						Path:    "/path4",
						Content: "# My NGINX config for /path4 location",
					},
				}, routes)
			},
		},
		{
			name:     "when a required value is not in the ConfigMap",
			instance: "instance3",
			assertion: func(t *testing.T, err error, routes []Route) {
				assert.Error(t, err)
			},
		},
		{
			name:     "when a ConfigMap of a required value is not found",
			instance: "instance4",
			assertion: func(t *testing.T, err error, routes []Route) {
				assert.Error(t, err)
				assert.True(t, k8sErrors.IsNotFound(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &k8sRpaasManager{cli: fake.NewFakeClientWithScheme(scheme, resources...)}
			routes, err := manager.GetRoutes(context.Background(), tt.instance)
			tt.assertion(t, err, routes)
		})
	}
}

func Test_k8sRpaasManager_UpdateRoute(t *testing.T) {
	instance1 := newEmptyRpaasInstance()

	instance2 := newEmptyRpaasInstance()
	instance2.Name = "another-instance"
	instance2.Spec.Locations = []v1alpha1.Location{
		{
			Path: "/path1",
			Content: &v1alpha1.Value{
				Value: "# My NGINX config for /path1 location",
			},
		},
		{
			Path:        "/path2",
			Destination: "app2.tsuru.example.com",
		},
		{
			Path:        "/path3",
			Destination: "app2.tsuru.example.com",
			ForceHTTPS:  true,
		},
		{
			Path: "/path4",
			Content: &v1alpha1.Value{
				ValueFrom: &v1alpha1.ValueSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "my-locations",
						},
						Key: "path1",
					},
				},
			},
		},
	}

	cm := newEmptyLocations()
	cm.Name = "another-instance-locations"
	cm.Data = map[string]string{
		"_path1": "# My NGINX config for /path1 location",
	}

	scheme := newScheme()
	resources := []runtime.Object{instance1, instance2, cm}

	tests := []struct {
		name      string
		instance  string
		route     Route
		assertion func(t *testing.T, err error, ri *v1alpha1.RpaasInstance, locations *corev1.ConfigMap)
	}{
		{
			name:     "when instance not found",
			instance: "instance-not-found",
			assertion: func(t *testing.T, err error, _ *v1alpha1.RpaasInstance, _ *corev1.ConfigMap) {
				assert.Error(t, err)
				assert.True(t, IsNotFoundError(err))
			},
		},
		{
			name:     "when path is not defined",
			instance: "my-instance",
			assertion: func(t *testing.T, err error, _ *v1alpha1.RpaasInstance, _ *corev1.ConfigMap) {
				assert.Error(t, err)
				assert.True(t, IsValidationError(err))
				assert.Equal(t, &ValidationError{Msg: "path is required"}, err)
			},
		},
		{
			name:     "when path is not valid",
			instance: "my-instance",
			route: Route{
				Path: "../../passwd",
			},
			assertion: func(t *testing.T, err error, _ *v1alpha1.RpaasInstance, _ *corev1.ConfigMap) {
				assert.Error(t, err)
				assert.True(t, IsValidationError(err))
				assert.Equal(t, &ValidationError{Msg: "invalid path format"}, err)
			},
		},
		{
			name:     "when both content and destination are not defined",
			instance: "my-instance",
			route: Route{
				Path: "/my/custom/path",
			},
			assertion: func(t *testing.T, err error, _ *v1alpha1.RpaasInstance, _ *corev1.ConfigMap) {
				assert.Error(t, err)
				assert.True(t, IsValidationError(err))
				assert.Equal(t, &ValidationError{Msg: "either content or destination are required"}, err)
			},
		},
		{
			name:     "when content and destination are defined at same time",
			instance: "my-instance",
			route: Route{
				Path:        "/my/custom/path",
				Destination: "app2.tsuru.example.com",
				Content:     "# My NGINX config at location context",
			},
			assertion: func(t *testing.T, err error, _ *v1alpha1.RpaasInstance, _ *corev1.ConfigMap) {
				assert.Error(t, err)
				assert.True(t, IsValidationError(err))
				assert.Equal(t, &ValidationError{Msg: "cannot set both content and destination"}, err)
			},
		},
		{
			name:     "when content and httpsOnly are defined at same time",
			instance: "my-instance",
			route: Route{
				Path:      "/my/custom/path",
				Content:   "# My NGINX config",
				HTTPSOnly: true,
			},
			assertion: func(t *testing.T, err error, _ *v1alpha1.RpaasInstance, _ *corev1.ConfigMap) {
				assert.Error(t, err)
				assert.True(t, IsValidationError(err))
				assert.Equal(t, &ValidationError{Msg: "cannot set both content and httpsonly"}, err)
			},
		},
		{
			name:     "when adding a new route with destination and httpsOnly",
			instance: "my-instance",
			route: Route{
				Path:        "/my/custom/path",
				Destination: "app2.tsuru.example.com",
				HTTPSOnly:   true,
			},
			assertion: func(t *testing.T, err error, ri *v1alpha1.RpaasInstance, _ *corev1.ConfigMap) {
				assert.NoError(t, err)
				assert.Equal(t, []v1alpha1.Location{
					{
						Path:        "/my/custom/path",
						Destination: "app2.tsuru.example.com",
						ForceHTTPS:  true,
					},
				}, ri.Spec.Locations)
			},
		},
		{
			name:     "when adding a route with custom NGINX config",
			instance: "my-instance",
			route: Route{
				Path:    "/custom/path",
				Content: "# My custom NGINX config",
			},
			assertion: func(t *testing.T, err error, ri *v1alpha1.RpaasInstance, _ *corev1.ConfigMap) {
				assert.NoError(t, err)
				assert.Len(t, ri.Spec.Locations, 1)
				assert.Equal(t, "/custom/path", ri.Spec.Locations[0].Path)
				assert.Equal(t, "# My custom NGINX config", ri.Spec.Locations[0].Content.Value)
			},
		},
		{
			name:     "when updating destination and httpsOnly fields of an existing route",
			instance: "another-instance",
			route: Route{
				Path:        "/path2",
				Destination: "another-app.tsuru.example.com",
				HTTPSOnly:   true,
			},
			assertion: func(t *testing.T, err error, ri *v1alpha1.RpaasInstance, locations *corev1.ConfigMap) {
				assert.NoError(t, err)
				assert.Len(t, ri.Spec.Locations, 4)
				assert.Equal(t, v1alpha1.Location{
					Path:        "/path2",
					Destination: "another-app.tsuru.example.com",
					ForceHTTPS:  true,
				}, ri.Spec.Locations[1])
			},
		},
		{
			name:     "when updating the NGINX configuration content",
			instance: "another-instance",
			route: Route{
				Path:    "/path1",
				Content: "# My new NGINX configuration",
			},
			assertion: func(t *testing.T, err error, ri *v1alpha1.RpaasInstance, locations *corev1.ConfigMap) {
				assert.NoError(t, err)
				assert.Equal(t, v1alpha1.Location{
					Path: "/path1",
					Content: &v1alpha1.Value{
						Value: "# My new NGINX configuration",
					},
				}, ri.Spec.Locations[0])
			},
		},
		{
			name:     "when updating a route to use destination instead of content",
			instance: "another-instance",
			route: Route{
				Path:        "/path1",
				Destination: "app1.tsuru.example.com",
				HTTPSOnly:   true,
			},
			assertion: func(t *testing.T, err error, ri *v1alpha1.RpaasInstance, _ *corev1.ConfigMap) {
				assert.NoError(t, err)
				assert.Equal(t, v1alpha1.Location{
					Path:        "/path1",
					Destination: "app1.tsuru.example.com",
					ForceHTTPS:  true,
				}, ri.Spec.Locations[0])
			},
		},
		{
			name:     "when updating a route to use destination instead of content",
			instance: "another-instance",
			route: Route{
				Path:    "/path2",
				Content: "# My new NGINX configuration",
			},
			assertion: func(t *testing.T, err error, ri *v1alpha1.RpaasInstance, _ *corev1.ConfigMap) {
				assert.NoError(t, err)
				assert.Equal(t, v1alpha1.Location{
					Path: "/path2",
					Content: &v1alpha1.Value{
						Value: "# My new NGINX configuration",
					},
				}, ri.Spec.Locations[1])
			},
		},
		{
			name:     "when updating a route which its Content was into ConfigMap",
			instance: "another-instance",
			route: Route{
				Path:    "/path4",
				Content: "# My new NGINX configuration",
			},
			assertion: func(t *testing.T, err error, ri *v1alpha1.RpaasInstance, _ *corev1.ConfigMap) {
				assert.NoError(t, err)
				assert.Equal(t, v1alpha1.Location{
					Path: "/path4",
					Content: &v1alpha1.Value{
						Value: "# My new NGINX configuration",
					},
				}, ri.Spec.Locations[3])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &k8sRpaasManager{cli: fake.NewFakeClientWithScheme(scheme, resources...)}
			err := manager.UpdateRoute(context.Background(), tt.instance, tt.route)
			ri := &v1alpha1.RpaasInstance{}
			if err == nil {
				newErr := manager.cli.Get(context.Background(), types.NamespacedName{Name: tt.instance, Namespace: namespaceName()}, ri)
				require.NoError(t, newErr)
			}
			tt.assertion(t, err, ri, nil)
		})
	}
}

func Test_getPlan(t *testing.T) {
	tests := []struct {
		name      string
		plan      string
		resources []runtime.Object
		assertion func(t *testing.T, err error, p *v1alpha1.RpaasPlan)
	}{
		{
			name:      "when plan does not exist",
			plan:      "unknown-plan",
			resources: []runtime.Object{},
			assertion: func(t *testing.T, err error, p *v1alpha1.RpaasPlan) {
				assert.Error(t, err)
				assert.Equal(t, NotFoundError{Msg: "plan \"unknown-plan\" not found"}, err)
			},
		},
		{
			name: "when plan is found by name",
			plan: "xxl",
			resources: []runtime.Object{
				&v1alpha1.RpaasPlan{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "xxl",
						Namespace: namespaceName(),
					},
				},
			},
			assertion: func(t *testing.T, err error, p *v1alpha1.RpaasPlan) {
				assert.NoError(t, err)
				assert.NotNil(t, p)
				assert.Equal(t, p.Name, "xxl")
			},
		},
		{
			name: "when plan is not set and there is a default plan",
			resources: []runtime.Object{
				&v1alpha1.RpaasPlan{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "some-default-plan",
						Namespace: namespaceName(),
					},
					Spec: v1alpha1.RpaasPlanSpec{
						Default: true,
					},
				},
			},
			assertion: func(t *testing.T, err error, p *v1alpha1.RpaasPlan) {
				assert.NoError(t, err)
				assert.NotNil(t, p)
				assert.Equal(t, p.Name, "some-default-plan")
			},
		},
		{
			name: "when plan is not set and there is no default plan",
			resources: []runtime.Object{
				&v1alpha1.RpaasPlan{
					ObjectMeta: metav1.ObjectMeta{Name: "plan1"},
				},
				&v1alpha1.RpaasPlan{
					ObjectMeta: metav1.ObjectMeta{Name: "plan2"},
				},
			},
			assertion: func(t *testing.T, err error, p *v1alpha1.RpaasPlan) {
				assert.Error(t, err)
				assert.Equal(t, NotFoundError{Msg: "no default plan found"}, err)
			},
		},
		{
			name: "when plan is not set and there are more than one default plan",
			resources: []runtime.Object{
				&v1alpha1.RpaasPlan{
					ObjectMeta: metav1.ObjectMeta{Name: "plan1"},
					Spec: v1alpha1.RpaasPlanSpec{
						Default: true,
					},
				},
				&v1alpha1.RpaasPlan{
					ObjectMeta: metav1.ObjectMeta{Name: "plan2"},
					Spec: v1alpha1.RpaasPlanSpec{
						Default: true,
					},
				},
			},
			assertion: func(t *testing.T, err error, p *v1alpha1.RpaasPlan) {
				assert.Error(t, err)
				assert.Error(t, ConflictError{Msg: "several default plans found: [plan1, plan2]"}, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &k8sRpaasManager{cli: fake.NewFakeClientWithScheme(newScheme(), tt.resources...)}
			p, err := manager.getPlan(context.Background(), tt.plan)
			tt.assertion(t, err, p)
		})
	}
}

func Test_isPathValid(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{
			path:     "../../passwd",
			expected: false,
		},
		{
			path:     "/bin/bash",
			expected: false,
		},
		{
			path:     "./subdir/file.txt",
			expected: true,
		},
		{
			path:     "..data/test",
			expected: false,
		},
		{
			path:     "subdir/my-file..txt",
			expected: false,
		},
		{
			path:     "my-file.txt",
			expected: true,
		},
		{
			path:     "path/to/my/file.txt",
			expected: true,
		},
		{
			path:     ".my-hidden-file",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			assert.Equal(t, tt.expected, isPathValid(tt.path))
		})
	}
}

func Test_convertPathToConfigMapKey(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{
			path:     "path/to/my-file.txt",
			expected: "path_to_my-file.txt",
		},
		{
			path:     "FILE@master.html",
			expected: "FILE_master.html",
		},
		{
			path:     "my new index.html",
			expected: "my_new_index.html",
		},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			assert.Equal(t, tt.expected, convertPathToConfigMapKey(tt.path))
		})
	}
}

func Test_k8sRpaasManager_CreateInstance(t *testing.T) {
	resources := []runtime.Object{
		&v1alpha1.RpaasPlan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "plan1",
				Namespace: namespaceName(),
			},
			Spec: v1alpha1.RpaasPlanSpec{
				Default: true,
			},
		},
		&v1alpha1.RpaasInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "r0",
				Namespace: namespaceName(),
				Labels: map[string]string{
					"rpaas.extensions.tsuru.io/service-name":  "rpaasv2",
					"rpaas.extensions.tsuru.io/instance-name": "r0",
					"rpaas_service":  "rpaasv2",
					"rpaas_instance": "r0",
				},
			},
			Spec: v1alpha1.RpaasInstanceSpec{},
		},
	}
	config.Set(config.RpaasConfig{
		ServiceName: "rpaasv2",
		Flavors: []config.FlavorConfig{
			{
				Name: "strawberry",
				Spec: v1alpha1.RpaasPlanSpec{
					Config: v1alpha1.NginxConfig{
						CacheEnabled: v1alpha1.Bool(false),
					},
				},
			},
		},
		TeamAffinity: map[string]corev1.Affinity{
			"team-one": {
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "machine-type",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"ultra-fast-io"},
									},
								},
							},
						},
					},
				},
			},
		},
	})
	defer config.Set(config.RpaasConfig{})
	one := int32(1)
	tests := []struct {
		name          string
		args          CreateArgs
		expected      v1alpha1.RpaasInstance
		expectedError string
	}{
		{
			name:          "without name",
			args:          CreateArgs{},
			expectedError: `name is required`,
		},
		{
			name:          "without team",
			args:          CreateArgs{Name: "r1"},
			expectedError: `team name is required`,
		},
		{
			name:          "invalid plan",
			args:          CreateArgs{Name: "r1", Team: "t1", Plan: "aaaaa"},
			expectedError: `invalid plan`,
		},
		{
			name:          "invalid flavor",
			args:          CreateArgs{Name: "r1", Team: "t1", Tags: []string{"flavor=aaaaa"}},
			expectedError: `flavor "aaaaa" not found`,
		},
		{
			name:          "override and flavor",
			args:          CreateArgs{Name: "r1", Team: "t1", Tags: []string{"flavor=strawberry", `plan-override={"config": {"cacheEnabled": false}}`}},
			expectedError: `cannot set both plan-override and flavor`,
		},
		{
			name:          "instance already exists",
			args:          CreateArgs{Name: "r0", Team: "t2"},
			expectedError: `rpaas instance named "r0" already exists`,
		},
		{
			name: "simplest",
			args: CreateArgs{Name: "r1", Team: "t1"},
			expected: v1alpha1.RpaasInstance{
				TypeMeta: metav1.TypeMeta{
					Kind:       "RpaasInstance",
					APIVersion: "extensions.tsuru.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "rpaasv2",
					Annotations: map[string]string{
						"rpaas.extensions.tsuru.io/description": "",
						"rpaas.extensions.tsuru.io/tags":        "",
						"rpaas.extensions.tsuru.io/team-owner":  "t1",
					},
					Labels: map[string]string{
						"rpaas.extensions.tsuru.io/service-name":  "rpaasv2",
						"rpaas.extensions.tsuru.io/instance-name": "r1",
						"rpaas.extensions.tsuru.io/team-owner":    "t1",
						"rpaas_service":                           "rpaasv2",
						"rpaas_instance":                          "r1",
					},
				},
				Spec: v1alpha1.RpaasInstanceSpec{
					Replicas: &one,
					PlanName: "plan1",
					Service: &nginxv1alpha1.NginxService{
						Type: corev1.ServiceTypeLoadBalancer,
						Labels: map[string]string{
							"rpaas.extensions.tsuru.io/service-name":  "rpaasv2",
							"rpaas.extensions.tsuru.io/instance-name": "r1",
							"rpaas.extensions.tsuru.io/team-owner":    "t1",
							"rpaas_service":                           "rpaasv2",
							"rpaas_instance":                          "r1",
						},
					},
					PodTemplate: nginxv1alpha1.NginxPodTemplateSpec{
						Labels: map[string]string{
							"rpaas.extensions.tsuru.io/service-name":  "rpaasv2",
							"rpaas.extensions.tsuru.io/instance-name": "r1",
							"rpaas.extensions.tsuru.io/team-owner":    "t1",
							"rpaas_service":                           "rpaasv2",
							"rpaas_instance":                          "r1",
						},
					},
				},
			},
		},
		{
			name: "with override",
			args: CreateArgs{Name: "r1", Team: "t1", Tags: []string{`plan-override={"config": {"cacheEnabled": false}}`}},
			expected: v1alpha1.RpaasInstance{
				TypeMeta: metav1.TypeMeta{
					Kind:       "RpaasInstance",
					APIVersion: "extensions.tsuru.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "rpaasv2",
					Annotations: map[string]string{
						"rpaas.extensions.tsuru.io/description": "",
						"rpaas.extensions.tsuru.io/tags":        `plan-override={"config": {"cacheEnabled": false}}`,
						"rpaas.extensions.tsuru.io/team-owner":  "t1",
					},
					Labels: map[string]string{
						"rpaas.extensions.tsuru.io/service-name":  "rpaasv2",
						"rpaas.extensions.tsuru.io/instance-name": "r1",
						"rpaas.extensions.tsuru.io/team-owner":    "t1",
						"rpaas_service":                           "rpaasv2",
						"rpaas_instance":                          "r1",
					},
				},
				Spec: v1alpha1.RpaasInstanceSpec{
					Replicas: &one,
					PlanName: "plan1",
					Service: &nginxv1alpha1.NginxService{
						Type: corev1.ServiceTypeLoadBalancer,
						Labels: map[string]string{
							"rpaas.extensions.tsuru.io/service-name":  "rpaasv2",
							"rpaas.extensions.tsuru.io/instance-name": "r1",
							"rpaas.extensions.tsuru.io/team-owner":    "t1",
							"rpaas_service":                           "rpaasv2",
							"rpaas_instance":                          "r1",
						},
					},
					PlanTemplate: &v1alpha1.RpaasPlanSpec{
						Config: v1alpha1.NginxConfig{
							CacheEnabled: v1alpha1.Bool(false),
						},
					},
					PodTemplate: nginxv1alpha1.NginxPodTemplateSpec{
						Labels: map[string]string{
							"rpaas.extensions.tsuru.io/service-name":  "rpaasv2",
							"rpaas.extensions.tsuru.io/instance-name": "r1",
							"rpaas.extensions.tsuru.io/team-owner":    "t1",
							"rpaas_service":                           "rpaasv2",
							"rpaas_instance":                          "r1",
						},
					},
				},
			},
		},
		{
			name: "with flavor",
			args: CreateArgs{Name: "r1", Team: "t1", Tags: []string{"flavor=strawberry"}},
			expected: v1alpha1.RpaasInstance{
				TypeMeta: metav1.TypeMeta{
					Kind:       "RpaasInstance",
					APIVersion: "extensions.tsuru.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "rpaasv2",
					Annotations: map[string]string{
						"rpaas.extensions.tsuru.io/description": "",
						"rpaas.extensions.tsuru.io/tags":        "flavor=strawberry",
						"rpaas.extensions.tsuru.io/team-owner":  "t1",
					},
					Labels: map[string]string{
						"rpaas.extensions.tsuru.io/service-name":  "rpaasv2",
						"rpaas.extensions.tsuru.io/instance-name": "r1",
						"rpaas.extensions.tsuru.io/team-owner":    "t1",
						"rpaas_service":                           "rpaasv2",
						"rpaas_instance":                          "r1",
					},
				},
				Spec: v1alpha1.RpaasInstanceSpec{
					Replicas: &one,
					PlanName: "plan1",
					Service: &nginxv1alpha1.NginxService{
						Type: corev1.ServiceTypeLoadBalancer,
						Labels: map[string]string{
							"rpaas.extensions.tsuru.io/service-name":  "rpaasv2",
							"rpaas.extensions.tsuru.io/instance-name": "r1",
							"rpaas.extensions.tsuru.io/team-owner":    "t1",
							"rpaas_service":                           "rpaasv2",
							"rpaas_instance":                          "r1",
						},
					},
					PlanTemplate: &v1alpha1.RpaasPlanSpec{
						Config: v1alpha1.NginxConfig{
							CacheEnabled: v1alpha1.Bool(false),
						},
					},
					PodTemplate: nginxv1alpha1.NginxPodTemplateSpec{
						Labels: map[string]string{
							"rpaas.extensions.tsuru.io/service-name":  "rpaasv2",
							"rpaas.extensions.tsuru.io/instance-name": "r1",
							"rpaas.extensions.tsuru.io/team-owner":    "t1",
							"rpaas_service":                           "rpaasv2",
							"rpaas_instance":                          "r1",
						},
					},
				},
			},
		},
		{
			name: "with team affinity",
			args: CreateArgs{Name: "r1", Team: "team-one"},
			expected: v1alpha1.RpaasInstance{
				TypeMeta: metav1.TypeMeta{
					Kind:       "RpaasInstance",
					APIVersion: "extensions.tsuru.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "rpaasv2",
					Annotations: map[string]string{
						"rpaas.extensions.tsuru.io/description": "",
						"rpaas.extensions.tsuru.io/tags":        "",
						"rpaas.extensions.tsuru.io/team-owner":  "team-one",
					},
					Labels: map[string]string{
						"rpaas.extensions.tsuru.io/service-name":  "rpaasv2",
						"rpaas.extensions.tsuru.io/instance-name": "r1",
						"rpaas.extensions.tsuru.io/team-owner":    "team-one",
						"rpaas_service":                           "rpaasv2",
						"rpaas_instance":                          "r1",
					},
				},
				Spec: v1alpha1.RpaasInstanceSpec{
					Replicas: &one,
					PlanName: "plan1",
					Service: &nginxv1alpha1.NginxService{
						Type: corev1.ServiceTypeLoadBalancer,
						Labels: map[string]string{
							"rpaas.extensions.tsuru.io/service-name":  "rpaasv2",
							"rpaas.extensions.tsuru.io/instance-name": "r1",
							"rpaas.extensions.tsuru.io/team-owner":    "team-one",
							"rpaas_service":                           "rpaasv2",
							"rpaas_instance":                          "r1",
						},
					},
					PodTemplate: nginxv1alpha1.NginxPodTemplateSpec{
						Affinity: &corev1.Affinity{
							NodeAffinity: &corev1.NodeAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "machine-type",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{"ultra-fast-io"},
												},
											},
										},
									},
								},
							},
						},
						Labels: map[string]string{
							"rpaas.extensions.tsuru.io/service-name":  "rpaasv2",
							"rpaas.extensions.tsuru.io/instance-name": "r1",
							"rpaas.extensions.tsuru.io/team-owner":    "team-one",
							"rpaas_service":                           "rpaasv2",
							"rpaas_instance":                          "r1",
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := newScheme()
			manager := &k8sRpaasManager{cli: fake.NewFakeClientWithScheme(scheme, resources...)}
			err := manager.CreateInstance(context.Background(), tt.args)
			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Regexp(t, tt.expectedError, err.Error())
			} else {
				require.NoError(t, err)
				result, err := manager.GetInstance(context.Background(), tt.args.Name)
				require.NoError(t, err)
				assert.Equal(t, &tt.expected, result)
			}
		})
	}
}

func Test_k8sRpaasManager_UpdateInstance(t *testing.T) {
	instance1 := newEmptyRpaasInstance()
	instance1.Name = "instance1"
	instance1.Labels = labelsForRpaasInstance(instance1.Name)
	instance1.Labels["rpaas.extensions.tsuru.io/team-owner"] = "team-one"
	instance1.Annotations = map[string]string{
		"rpaas.extensions.tsuru.io/description": "Description about instance1",
		"rpaas.extensions.tsuru.io/tags":        "tag1,tag2",
	}
	instance1.Spec.PlanName = "plan1"

	podLabels := mergeMap(instance1.Labels, map[string]string{"pod-label-1": "v1"})

	instance1.Spec.PodTemplate = nginxv1alpha1.NginxPodTemplateSpec{
		Annotations: instance1.Annotations,
		Labels:      podLabels,
	}

	plan1 := &v1alpha1.RpaasPlan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "extensions.tsuru.io/v1alpha1",
			Kind:       "RpaasPlan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "plan1",
			Namespace: namespaceName(),
		},
	}

	plan2 := &v1alpha1.RpaasPlan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "extensions.tsuru.io/v1alpha1",
			Kind:       "RpaasPlan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "plan2",
			Namespace: namespaceName(),
		},
	}

	resources := []runtime.Object{instance1, plan1, plan2}

	tests := []struct {
		name      string
		instance  string
		args      UpdateInstanceArgs
		assertion func(t *testing.T, err error, instance *v1alpha1.RpaasInstance)
	}{
		{
			name:     "when the newer plan does not exist",
			instance: "instance1",
			args: UpdateInstanceArgs{
				Plan: "not-found",
			},
			assertion: func(t *testing.T, err error, instance *v1alpha1.RpaasInstance) {
				require.Error(t, err)
				assert.Error(t, NotFoundError{
					Msg: `plan "not-found" not found`,
				}, err)
			},
		},
		{
			name:     "when successfully updating an instance",
			instance: "instance1",
			args: UpdateInstanceArgs{
				Description: "Another description",
				Plan:        "plan2",
				Tags:        []string{"tag3", "tag4", "tag5", `plan-override={"image": "my.registry.test/nginx:latest"}`},
				Team:        "team-two",
			},
			assertion: func(t *testing.T, err error, instance *v1alpha1.RpaasInstance) {
				require.NoError(t, err)
				require.NotNil(t, instance)
				assert.Equal(t, "plan2", instance.Spec.PlanName)
				require.NotNil(t, instance.Labels)
				assert.Equal(t, "team-two", instance.Labels["rpaas.extensions.tsuru.io/team-owner"])
				require.NotNil(t, instance.Annotations)
				assert.Equal(t, "Another description", instance.Annotations["rpaas.extensions.tsuru.io/description"])
				assert.Equal(t, `plan-override={"image": "my.registry.test/nginx:latest"},tag3,tag4,tag5`, instance.Annotations["rpaas.extensions.tsuru.io/tags"])
				assert.Equal(t, "team-two", instance.Annotations["rpaas.extensions.tsuru.io/team-owner"])
				require.NotNil(t, instance.Spec.PodTemplate)
				assert.Equal(t, "v1", instance.Spec.PodTemplate.Labels["pod-label-1"])
				assert.Equal(t, "team-two", instance.Spec.PodTemplate.Labels["rpaas.extensions.tsuru.io/team-owner"])
				assert.Equal(t, &v1alpha1.RpaasPlanSpec{Image: "my.registry.test/nginx:latest"}, instance.Spec.PlanTemplate)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &k8sRpaasManager{
				cli: fake.NewFakeClientWithScheme(newScheme(), resources...),
			}
			err := manager.UpdateInstance(context.TODO(), tt.instance, tt.args)
			instance := new(v1alpha1.RpaasInstance)
			if err == nil {
				nerr := manager.cli.Get(context.TODO(), types.NamespacedName{Name: tt.instance, Namespace: namespaceName()}, instance)
				require.NoError(t, nerr)
			}
			tt.assertion(t, err, instance)
		})
	}
}

func newScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	v1alpha1.SchemeBuilder.AddToScheme(scheme)
	nginxv1alpha1.SchemeBuilder.AddToScheme(scheme)
	return scheme
}
