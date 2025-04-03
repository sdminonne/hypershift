package controllers

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	. "github.com/onsi/gomega"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	"github.com/openshift/hypershift/support/releaseinfo"
	"github.com/openshift/hypershift/support/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestExtractMCOBinaries(t *testing.T) {
	testCases := []struct {
		name                  string
		mcoOSReleaseVersion   string
		cpoOSReleaseVersion   string
		expectedBinaryVersion string
		cacheFunc             regClient
	}{
		{
			name:                  "When both MCO and CPO are on RHEL 8, it should extract the RHEL 8 binaries with no prefix",
			mcoOSReleaseVersion:   "8.1",
			cpoOSReleaseVersion:   "8.2",
			expectedBinaryVersion: "rhel8",
		},
		{
			name:                  "When both MCO is in RHEL 8 and CPO on RHEL 9, it should extract the RHEL 9 binaries with the .rhel9 prefix",
			mcoOSReleaseVersion:   "8.1",
			cpoOSReleaseVersion:   "9.1",
			expectedBinaryVersion: "rhel9",
		},
		{
			name:                  "When MCO is in too old version and CPO on RHEL 9, and the RHEL 9 binaries do not exist it should extract the RHEL 8 binaries with no prefix",
			mcoOSReleaseVersion:   "8.0",
			cpoOSReleaseVersion:   "9.1",
			expectedBinaryVersion: "rhel8",
			cacheFunc: func(ctx context.Context, imageRef string, pullSecret []byte, file string, out io.Writer) error {
				switch file {
				case "usr/lib/os-release":
					_, err := out.Write([]byte(fmt.Sprintf("VERSION_ID=\"%s\"\n", "8.0")))
					return err
				case "usr/bin/machine-config-operator", "usr/bin/machine-config-controller", "usr/bin/machine-config-server":
					_, err := out.Write([]byte("rhel8"))
					return err
				case "usr/bin/machine-config-operator.rhel9", "usr/bin/machine-config-controller.rhel9", "usr/bin/machine-config-server.rhel9":
					return fmt.Errorf("file not found: %s", file)
				default:
					return fmt.Errorf("unexpected file: %s", file)
				}
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			tempDir, err := os.MkdirTemp("", "testExtractBinaries-*")
			if err != nil {
				t.Fatalf("Failed to create temporary directory: %v", err)
			}
			defer func(path string) {
				err = os.RemoveAll(path)
				g.Expect(err).ToNot(HaveOccurred())
			}(tempDir)

			// Set up the necessary variables for testing.
			ctx := context.Background()
			mcoImage := "fake"
			pullSecret := []byte{}
			binDir := filepath.Join(tempDir, "bin")
			err = os.Mkdir(binDir, 0755)
			g.Expect(err).ToNot(HaveOccurred())

			// Create a fake file cache that returns the expected binaries.
			imageFileCache := &imageFileCache{
				cacheMap: make(map[cacheKey]cacheValue),
				cacheDir: tempDir,
			}
			imageFileCache.regClient = func(ctx context.Context, imageRef string, pullSecret []byte, file string, out io.Writer) error {
				switch file {
				case "usr/lib/os-release":
					_, err := out.Write([]byte(fmt.Sprintf("VERSION_ID=\"%s\"\n", tc.mcoOSReleaseVersion)))
					return err
				case "usr/bin/machine-config-operator", "usr/bin/machine-config-controller", "usr/bin/machine-config-server":
					_, err := out.Write([]byte("rhel8"))
					return err
				case "usr/bin/machine-config-operator.rhel9", "usr/bin/machine-config-controller.rhel9", "usr/bin/machine-config-server.rhel9":
					_, err := out.Write([]byte("rhel9"))
					return err
				default:
					return fmt.Errorf("unexpected file: %s", file)
				}
			}

			// If the test case has a custom cache function, use it.
			// This is useful to simulate the case where the ocp release for the NodePool is too old that it doesn't have the RHEL binaries.
			if tc.cacheFunc != nil {
				imageFileCache.regClient = tc.cacheFunc
			}

			// Create a fake cpo os-release file
			cpoOSRelease := fmt.Sprintf("VERSION_ID=\"%s\"\n", tc.cpoOSReleaseVersion)
			cpoOSReleaseFilePath := filepath.Join(tempDir, "usr/lib/os-release")
			err = os.MkdirAll(filepath.Dir(cpoOSReleaseFilePath), 0755)
			g.Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(cpoOSReleaseFilePath, []byte(cpoOSRelease), 0644)
			g.Expect(err).NotTo(HaveOccurred())

			// Create a LocalIgnitionProvider instance for testing.
			provider := &LocalIgnitionProvider{
				ImageFileCache: imageFileCache,
				WorkDir:        tempDir,
			}

			// Call the extractMCOBinaries.
			err = provider.extractMCOBinaries(ctx, cpoOSReleaseFilePath, mcoImage, pullSecret, binDir)
			g.Expect(err).NotTo(HaveOccurred())

			// Verify the extracted binaries match the expected version.
			for _, name := range []string{"machine-config-operator", "machine-config-controller", "machine-config-server"} {
				filePath := filepath.Join(binDir, name)
				fileContent, err := os.ReadFile(filePath)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(string(fileContent)).To(Equal(tc.expectedBinaryVersion))
			}
		})
	}
}

type myFakeclient struct {
	client.WithWatch
}

func (c myFakeclient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if key.Namespace == "failed-pull-secret-ns" && key.Name == "pull-secret" {
		return fmt.Errorf("failed to get pull secret")
	}
	if key.Namespace == "missing-dockerconfigjson-key-ns" && key.Name == "pull-secret" {
		obj = &corev1.Secret{}
		return nil
	}
	if key.Namespace == "pull-secret-does-not-match-ns" && key.Name == "pull-secret" {
		obj.(*corev1.Secret).Data = map[string][]byte{
			corev1.DockerConfigJsonKey: []byte("data"),
		}
		return nil
	}
	if key.Namespace == "no-additional-trust-bundle-ns" {
		switch key.Name {
		case "pull-secret":
			{
				obj.(*corev1.Secret).Data = map[string][]byte{
					corev1.DockerConfigJsonKey: []byte("data"),
				}
				return nil
			}
		case additionalTrustBundleName:
			{
				return fmt.Errorf("error")
			}
		}
		return nil
	}
	panic("should not be here NS:" + key.Namespace + " Name:" + key.Name + " Kind" + reflect.TypeOf(obj).String())
	return nil
}

func TestLocalIgnitionProvider_GetPayload_clientErrors(t *testing.T) {
	type fields struct {
		Client                client.Client
		ReleaseProvider       releaseinfo.ProviderWithOpenShiftImageRegistryOverrides
		CloudProvider         hyperv1.PlatformType
		Namespace             string
		WorkDir               string
		PreserveOutput        bool
		FeatureGateManifest   string
		ImageMetadataProvider *util.RegistryClientImageMetadataProvider
		ImageFileCache        *imageFileCache
		lock                  sync.Mutex
	}
	type args struct {
		ctx                       context.Context
		releaseImage              string
		customConfig              string
		pullSecretHash            string
		additionalTrustBundleHash string
		hcConfigurationHash       string
	}
	tests := []struct {
		name      string
		fields    fields
		args      args
		want      []byte
		wantError bool
	}{
		{name: "Error: failed to get pull secret",
			fields: fields{
				Namespace: "failed-pull-secret-ns",
				lock:      sync.Mutex{},
			},
			args: args{
				ctx: context.TODO(),
			},
			want:      nil,
			wantError: true,
		},
		{
			name: "Error: pull secret missing .dockerconfigjson key",
			fields: fields{
				Namespace: "missing-dockerconfigjson-key-ns",
				lock:      sync.Mutex{},
			},
			args:      args{},
			wantError: true,
		},
		{
			name: "Error:  pull secret does not match",
			fields: fields{
				Namespace: "pull-secret-does-not-match-ns",
			},
			args: args{
				pullSecretHash: "wrong-pull-secret-hash",
				ctx:            context.TODO(),
			},
			want:      nil,
			wantError: true,
		},
		{
			name: "Error: no additional trust bundle",
			fields: fields{
				Namespace: "no-additional-trust-bundle-ns",
			},
			args: args{
				ctx: context.TODO(),
			},
			want:      nil,
			wantError: true,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			p := &LocalIgnitionProvider{
				Client:                myFakeclient{},
				ReleaseProvider:       tt.fields.ReleaseProvider,
				CloudProvider:         tt.fields.CloudProvider,
				Namespace:             tt.fields.Namespace,
				WorkDir:               tt.fields.WorkDir,
				PreserveOutput:        tt.fields.PreserveOutput,
				FeatureGateManifest:   tt.fields.FeatureGateManifest,
				ImageMetadataProvider: tt.fields.ImageMetadataProvider,
				ImageFileCache:        tt.fields.ImageFileCache,
				lock:                  tt.fields.lock,
			}
			got, err := p.GetPayload(tt.args.ctx, tt.args.releaseImage, tt.args.customConfig, tt.args.pullSecretHash, tt.args.additionalTrustBundleHash, tt.args.hcConfigurationHash)
			if (err != nil) != tt.wantError {
				t.Errorf("LocalIgnitionProvider.GetPayload() error = %v, wantErr %v", err, tt.wantError)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LocalIgnitionProvider.GetPayload() = %v, want %v", got, tt.want)
			}
		})
	}
}

func initSecretOrDie(namespace, name, key string, data []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			key: data,
		},
	}
}

func initconfigMapOrDie(namespace, name, key, data string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			key: data},
	}
}

func TestLocalIgnitionProvider_GetPayload(t *testing.T) {
	type fields struct {
		Client                client.Client
		ReleaseProvider       releaseinfo.ProviderWithOpenShiftImageRegistryOverrides
		CloudProvider         hyperv1.PlatformType
		Namespace             string
		WorkDir               string
		PreserveOutput        bool
		FeatureGateManifest   string
		ImageMetadataProvider *util.RegistryClientImageMetadataProvider
		ImageFileCache        *imageFileCache
		lock                  sync.Mutex
	}
	type args struct {
		ctx                       context.Context
		releaseImage              string
		customConfig              string
		pullSecretHash            string
		additionalTrustBundleHash string
		hcConfigurationHash       string
	}
	tests := []struct {
		name      string
		fields    fields
		args      args
		want      []byte
		objects   []client.Object
		wantError bool
	}{
		{name: "WIP:",
			fields: fields{
				Namespace: "the-namespace",
				lock:      sync.Mutex{},
			},
			args: args{
				ctx: context.TODO(),
			},
			want: nil,
			objects: []client.Object{
				initSecretOrDie("the-namespace", pullSecretName, corev1.DockerConfigJsonKey, []byte("data")),
				initconfigMapOrDie("the-namespace", additionalTrustBundleName, "ca-bundle.crt", "data"),
				initSecretOrDie("the-namespace", "bootstrap-kubeconfig", "kubeconfig", []byte("data")),
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		client := fake.NewClientBuilder().
			WithObjects(tt.objects...).
			Build()
		t.Run(tt.name, func(t *testing.T) {
			p := &LocalIgnitionProvider{
				Client:                client,
				ReleaseProvider:       tt.fields.ReleaseProvider,
				CloudProvider:         tt.fields.CloudProvider,
				Namespace:             tt.fields.Namespace,
				WorkDir:               tt.fields.WorkDir,
				PreserveOutput:        tt.fields.PreserveOutput,
				FeatureGateManifest:   tt.fields.FeatureGateManifest,
				ImageMetadataProvider: tt.fields.ImageMetadataProvider,
				ImageFileCache:        tt.fields.ImageFileCache,
				lock:                  tt.fields.lock,
			}
			got, err := p.GetPayload(tt.args.ctx, tt.args.releaseImage, tt.args.customConfig, tt.args.pullSecretHash, tt.args.additionalTrustBundleHash, tt.args.hcConfigurationHash)
			if (err != nil) != tt.wantError {
				t.Errorf("LocalIgnitionProvider.GetPayload() error = %v, wantErr %v", err, tt.wantError)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LocalIgnitionProvider.GetPayload() = %v, want %v", got, tt.want)
			}
		})
	}
}
