package releaseinfo

import (
	"testing"

	. "github.com/onsi/gomega"
	"github.com/openshift/hypershift/support/releaseinfo/fixtures"
)

func TestParseComponentVersionsLabel(t *testing.T) {
	tests := []struct {
		name         string
		label        string
		displayNames string
		expectError  bool
		expectedKeys []string
	}{
		{
			name:         "When display name contains a period it should succeed",
			label:        "rhel-coreos=49.98.202503100834-0",
			displayNames: "rhel-coreos=Red Hat Enterprise Linux CoreOS 9.8",
			expectError:  false,
			expectedKeys: []string{"rhel-coreos"},
		},
		{
			name:         "When display name contains parentheses and colons it should succeed",
			label:        "kubernetes=1.31.0",
			displayNames: "kubernetes=Kubernetes (upstream: v1.31)",
			expectError:  false,
			expectedKeys: []string{"kubernetes"},
		},
		{
			name:         "When display name contains invalid characters it should fail",
			label:        "rhel-coreos=49.98.202503100834-0",
			displayNames: "rhel-coreos=RHEL CoreOS <9.8>",
			expectError:  true,
		},
		{
			name:         "When display names is empty it should succeed",
			label:        "rhel-coreos=49.98.202503100834-0",
			displayNames: "",
			expectError:  false,
			expectedKeys: []string{"rhel-coreos"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			result, err := parseComponentVersionsLabel(tt.label, tt.displayNames)
			if tt.expectError {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
				for _, key := range tt.expectedKeys {
					g.Expect(result).To(HaveKey(key))
				}
			}
		})
	}
}

// TestReleaseInfoPowerVS test validates the presence of the powervs images in the 4.10 release
func TestReleaseInfoPowerVS(t *testing.T) {
	metadata, err := DeserializeImageMetadata(fixtures.CoreOSBootImagesYAML_4_10)
	if err != nil {
		t.Fatal(err)
	}
	arch, ok := metadata.Architectures["ppc64le"]
	if !ok {
		t.Fatal("metadata does not contain the ppc64le architecture")
	}
	if len(arch.Images.PowerVS.Regions) == 0 {
		t.Fatal("metadata does not contain any powervs regions")
	}
	for _, region := range arch.Images.PowerVS.Regions {
		if region.Release == "" || region.Object == "" || region.Bucket == "" || region.URL == "" {
			t.Fatalf("none of the fields in the image can be empty: %+v", region)
		}
	}
}

// TestReleaseInfoKubeVirt tests validates the presence of the kubevirt images
func TestReleaseInfoKubeVirt(t *testing.T) {
	metadata, err := DeserializeImageMetadata(fixtures.CoreOSBootImagesYAML_4_10)
	if err != nil {
		t.Fatal(err)
	}
	arch, ok := metadata.Architectures["x86_64"]
	if !ok {
		t.Fatal("metadata does not contain the x86_64 architecture")
	}
	if arch.Images.Kubevirt.DigestRef == "" {
		t.Fatal("metadata does not contain a digest ref for kubevirt")
	}
}
