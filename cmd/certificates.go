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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	//
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	//
	"github.com/admitikcore/admitik/internal/certificates"
	"github.com/admitikcore/admitik/internal/globals"
)

const (
	// certificateSecretKey is the key of the Secret that holds each part of the certificate
	certificateCAKey          = "ca.crt"
	certificateCertificateKey = "tls.crt"
	certificatePrivateKeyKey  = "tls.key"

	// certificateRetries is how many times the Secret is read before giving up
	certificateRetries = 3
)

// certificateOptions is where the certificate of the webhook server comes from.
// It is either a set of files, a Secret, or nothing at all
type certificateOptions struct {
	// Certificate provided as files
	CAFile, CertificateFile, PrivateKeyFile string

	// Secret that holds the certificate, in the namespace of Admitik
	SecretName string
	Namespace  string

	// Autogenerate creates the Secret when it does not exist
	Autogenerate bool

	// Hostname included in a generated certificate
	Hostname string
}

func (o certificateOptions) hasFiles() bool {
	return o.CAFile != "" || o.CertificateFile != "" || o.PrivateKeyFile != ""
}

// certificateData is the content of a certificate
type certificateData struct {
	ca          string
	certificate string
	privateKey  string
}

// setupCertificates returns the paths of the files the webhook server has to use, generating the
// certificate when it was asked for and it does not exist yet
func setupCertificates(options certificateOptions) (ca, certificate, privateKey string, err error) {

	if options.hasFiles() && options.SecretName != "" {
		return "", "", "", errors.New("a certificate from files and from a Secret are mutually exclusive")
	}

	if options.SecretName == "" {
		return options.CAFile, options.CertificateFile, options.PrivateKeyFile, nil
	}

	data, err := loadOrCreateCertificate(options)
	if err != nil {
		return "", "", "", err
	}

	return writeCertificateFiles(data)
}

// loadOrCreateCertificate reads the certificate from its Secret, creating it when it is missing
func loadOrCreateCertificate(options certificateOptions) (certificateData, error) {

	secret, err := readCertificateSecret(options)
	if err != nil {
		return certificateData{}, err
	}

	if secret != nil {
		return certificateFromSecret(secret)
	}

	if !options.Autogenerate {
		return certificateData{}, fmt.Errorf("secret '%s' does not exist and autogeneration is disabled", options.SecretName)
	}

	return createCertificateSecret(options)
}

// readCertificateSecret returns the Secret with the certificate, or nil when it does not exist
func readCertificateSecret(options certificateOptions) (*corev1.Secret, error) {

	var lastErr error
	for attempt := 0; attempt < certificateRetries; attempt++ {

		secret, err := globals.Application.KubeRawCoreClient.CoreV1().Secrets(options.Namespace).
			Get(globals.Application.Context, options.SecretName, metav1.GetOptions{})

		if err == nil {
			return secret, nil
		}
		if apierrors.IsNotFound(err) {
			return nil, nil
		}

		lastErr = err
	}

	return nil, fmt.Errorf("error reading the secret '%s': %w", options.SecretName, lastErr)
}

// certificateFromSecret extracts the certificate from the Secret
func certificateFromSecret(secret *corev1.Secret) (certificateData, error) {

	data := certificateData{}
	for key, target := range map[string]*string{
		certificateCAKey:          &data.ca,
		certificateCertificateKey: &data.certificate,
		certificatePrivateKeyKey:  &data.privateKey,
	} {
		value, found := secret.Data[key]
		if !found {
			return certificateData{}, fmt.Errorf("secret '%s' does not have '%s'", secret.Name, key)
		}

		*target = string(value)
	}

	return data, nil
}

// createCertificateSecret generates a certificate and stores it in a Secret
func createCertificateSecret(options certificateOptions) (certificateData, error) {

	ca, certificate, privateKey, err := certificates.GenerateCerts(dnsNames(options.Hostname))
	if err != nil {
		return certificateData{}, fmt.Errorf("error generating a self signed certificate: %w", err)
	}

	_, err = globals.Application.KubeRawCoreClient.CoreV1().Secrets(options.Namespace).
		Create(globals.Application.Context, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: options.SecretName, Namespace: options.Namespace},
			StringData: map[string]string{
				certificateCAKey:          ca,
				certificateCertificateKey: certificate,
				certificatePrivateKeyKey:  privateKey,
			},
		}, metav1.CreateOptions{})

	// Every replica generates a certificate when the Secret is missing, and only one of them wins.
	// The others read the one that won, so all of them serve the same certificate
	if apierrors.IsAlreadyExists(err) {
		secret, readErr := readCertificateSecret(options)
		if readErr != nil {
			return certificateData{}, readErr
		}
		if secret == nil {
			return certificateData{}, fmt.Errorf("the secret '%s' existed but it is not there anymore", options.SecretName)
		}

		return certificateFromSecret(secret)
	}

	if err != nil {
		return certificateData{}, fmt.Errorf("error storing the certificate in the secret '%s': %w", options.SecretName, err)
	}

	return certificateData{ca: ca, certificate: certificate, privateKey: privateKey}, nil
}

// dnsNames returns the names a generated certificate has to be valid for
func dnsNames(hostname string) []string {

	names := []string{"localhost", hostname}

	if strings.HasSuffix(hostname, ".cluster.local") {
		names = append(names, strings.TrimSuffix(hostname, ".cluster.local"))
	}
	if strings.HasSuffix(hostname, ".svc") {
		names = append(names, hostname+".cluster.local")
	}

	return names
}

// writeCertificateFiles writes the certificate to a temporary directory, because the TLS server
// reads it from files
func writeCertificateFiles(data certificateData) (ca, certificate, privateKey string, err error) {

	directory := os.TempDir()

	for _, file := range []struct {
		path    *string
		name    string
		content string
	}{
		{path: &ca, name: certificateCAKey, content: data.ca},
		{path: &certificate, name: certificateCertificateKey, content: data.certificate},
		{path: &privateKey, name: certificatePrivateKeyKey, content: data.privateKey},
	} {
		*file.path = filepath.Join(directory, file.name)

		if err := os.WriteFile(*file.path, []byte(file.content), 0600); err != nil {
			return "", "", "", fmt.Errorf("error writing the certificate file '%s': %w", *file.path, err)
		}
	}

	return ca, certificate, privateKey, nil
}
