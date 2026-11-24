/*
Copyright 2024.

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

package main

import (
	"os"
	"slices"
	"testing"

	//
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	//
	"github.com/freepik-company/admitik/internal/globals"
)

func TestDNSNames(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		expected []string
	}{
		{
			name:     "short service name",
			hostname: "webhooks.admitik.svc",
			expected: []string{"localhost", "webhooks.admitik.svc", "webhooks.admitik.svc.cluster.local"},
		},
		{
			name:     "fully qualified service name",
			hostname: "webhooks.admitik.svc.cluster.local",
			expected: []string{"localhost", "webhooks.admitik.svc.cluster.local", "webhooks.admitik.svc"},
		},
		{
			name:     "a plain host",
			hostname: "192.168.1.10",
			expected: []string{"localhost", "192.168.1.10"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := dnsNames(test.hostname); !slices.Equal(got, test.expected) {
				t.Errorf("got %v, expected %v", got, test.expected)
			}
		})
	}
}

func TestCertificateFromSecret(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "certs"},
		Data: map[string][]byte{
			certificateCAKey:          []byte("ca"),
			certificateCertificateKey: []byte("certificate"),
			certificatePrivateKeyKey:  []byte("key"),
		},
	}

	data, err := certificateFromSecret(secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if data.ca != "ca" || data.certificate != "certificate" || data.privateKey != "key" {
		t.Errorf("got %+v, expected the content of the secret", data)
	}
}

func TestCertificateFromSecretWithAMissingKey(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "certs"},
		Data:       map[string][]byte{certificateCAKey: []byte("ca")},
	}

	if _, err := certificateFromSecret(secret); err == nil {
		t.Error("expected an error for a secret without tls.crt and tls.key")
	}
}

func TestSetupCertificatesWithoutSecret(t *testing.T) {
	ca, certificate, privateKey, err := setupCertificates(certificateOptions{
		CAFile:          "/tmp/ca.crt",
		CertificateFile: "/tmp/tls.crt",
		PrivateKeyFile:  "/tmp/tls.key",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ca != "/tmp/ca.crt" || certificate != "/tmp/tls.crt" || privateKey != "/tmp/tls.key" {
		t.Errorf("got the files %s, %s, %s", ca, certificate, privateKey)
	}
}

func TestSetupCertificatesRefusesFilesAndSecretTogether(t *testing.T) {
	_, _, _, err := setupCertificates(certificateOptions{
		CAFile:     "/tmp/ca.crt",
		SecretName: "certs",
	})

	if err == nil {
		t.Error("expected an error")
	}
}

func TestWriteCertificateFiles(t *testing.T) {
	defer func() {
		for _, name := range []string{certificateCAKey, certificateCertificateKey, certificatePrivateKeyKey} {
			_ = os.Remove(os.TempDir() + "/" + name)
		}
	}()

	ca, certificate, privateKey, err := writeCertificateFiles(certificateData{
		ca:          "ca",
		certificate: "certificate",
		privateKey:  "key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for path, expected := range map[string]string{
		ca:          "ca",
		certificate: "certificate",
		privateKey:  "key",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("unexpected error reading %s: %v", path, err)
		}
		if string(content) != expected {
			t.Errorf("got %q in %s, expected %q", string(content), path, expected)
		}
	}
}

func TestCreateCertificateSecretWhenAnotherReplicaWonTheRace(t *testing.T) {
	// El Secret ya existe porque otro pod lo creo mientras este generaba el suyo
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "certs", Namespace: "admitik-system"},
		Data: map[string][]byte{
			certificateCAKey:          []byte("from-the-winner"),
			certificateCertificateKey: []byte("from-the-winner"),
			certificatePrivateKeyKey:  []byte("from-the-winner"),
		},
	}

	previous := globals.Application.KubeRawCoreClient
	globals.Application.KubeRawCoreClient = k8sfake.NewSimpleClientset(existing)
	defer func() { globals.Application.KubeRawCoreClient = previous }()

	data, err := createCertificateSecret(certificateOptions{
		SecretName: "certs",
		Namespace:  "admitik-system",
		Hostname:   "webhooks.admitik.svc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if data.ca != "from-the-winner" {
		t.Errorf("got %q, expected the certificate that is in the secret", data.ca)
	}
}
