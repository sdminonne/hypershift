//go:build e2ev2 && backuprestore

package backuprestore

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	"github.com/openshift/hypershift/test/e2e/v2/internal"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// EtcdSnapshotBackupOptions returns an OADPBackupOptions configured for etcd snapshot mode.
// In this mode etcd is backed up via HCPEtcdBackup CRD snapshots instead of PV volume
// snapshots. The manifest produced by the CLI differs from the default PV-based backup:
//   - snapshotVolumes: false
//   - snapshotMoveData: false (no dataMover, no csiSnapshotTimeout)
//   - itemOperationTimeout: 4h0m0s
//   - excludedResources: [] (empty)
//   - includedResources: short-name resources without PV-related types, plus namespaces
//
// See https://github.com/openshift/hypershift/pull/8232 for the full CLI implementation.
//
// NOTE: snapshotVolumes, itemOperationTimeout, excludedResources, and the etcd-snapshot
// resource list are handled by the --use-etcd-snapshot CLI flag (PR #8232). Until that
// flag is available in the OADPBackupOptions struct, only SnapshotMoveData is set here.
// TODO(jparrill): Once PR #8232 is merged and OADPBackupOptions gains a UseEtcdSnapshot
// field, set it here and remove the explicit SnapshotMoveData override.
func EtcdSnapshotBackupOptions(name, hcName, hcNamespace, storageLocation string) *OADPBackupOptions {
	return &OADPBackupOptions{
		Name:             name,
		HCName:           hcName,
		HCNamespace:      hcNamespace,
		StorageLocation:  storageLocation,
		SnapshotMoveData: ptr.To(false),
	}
}

// EtcdSnapshotRestoreOptions returns an OADPRestoreOptions configured for etcd snapshot mode.
// In this mode the restore manifest differs from the default PV-based restore:
//   - restorePVs: false
//   - cleanupBeforeRestore: CleanupRestored
//   - veleroManagedClustersBackupName / veleroCredentialsBackupName / veleroResourcesBackupName
//     are all set to the backup name
//   - excludedResources omits csinodes, volumeattachments, and backuprepositories
//     (keeping only nodes, events, events.events.k8s.io, backups/restores/resticrepositories.velero.io)
//
// See https://github.com/openshift/hypershift/pull/8232 for the full CLI implementation.
//
// NOTE: cleanupBeforeRestore, velero*BackupName fields, and the reduced excludedResources
// list are handled by the --use-etcd-snapshot CLI flag (PR #8232). Until that flag is
// available in the OADPRestoreOptions struct, only RestorePVs and PreserveNodePorts are set.
// TODO(jparrill): Once PR #8232 is merged and OADPRestoreOptions gains a UseEtcdSnapshot
// field, set it here and remove the explicit RestorePVs/PreserveNodePorts overrides.
func EtcdSnapshotRestoreOptions(name, fromBackup, hcName, hcNamespace string) *OADPRestoreOptions {
	return &OADPRestoreOptions{
		Name:              name,
		FromBackup:        fromBackup,
		HCName:            hcName,
		HCNamespace:       hcNamespace,
		RestorePVs:        ptr.To(false),
		PreserveNodePorts: ptr.To(true),
	}
}

// WaitForHCPEtcdBackupCondition waits for an HCPEtcdBackup resource with the given name
// to have a BackupCompleted condition with the specified status.
func WaitForHCPEtcdBackupCondition(testCtx *internal.TestContext, backupName string, expectedStatus metav1.ConditionStatus) error {
	return wait.PollUntilContextTimeout(testCtx.Context, PollInterval, BackupTimeout, true, func(ctx context.Context) (bool, error) {
		hcpEtcdBackupList := &hyperv1.HCPEtcdBackupList{}
		if err := testCtx.MgmtClient.List(ctx, hcpEtcdBackupList, crclient.InNamespace(testCtx.ControlPlaneNamespace)); err != nil {
			return false, fmt.Errorf("failed to list HCPEtcdBackup resources: %w", err)
		}

		for _, backup := range hcpEtcdBackupList.Items {
			if backup.Name != backupName {
				continue
			}
			condition := meta.FindStatusCondition(backup.Status.Conditions, string(hyperv1.BackupCompleted))
			if condition == nil {
				return false, nil
			}
			if condition.Status == expectedStatus {
				return true, nil
			}
			// If the condition is explicitly False, the backup failed - stop polling.
			if expectedStatus == metav1.ConditionTrue && condition.Status == metav1.ConditionFalse {
				return false, fmt.Errorf("HCPEtcdBackup %s has BackupCompleted=False: reason=%s, message=%s",
					backupName, condition.Reason, condition.Message)
			}
			return false, nil
		}
		return false, nil
	})
}

// VerifyEtcdInitLogs retrieves the etcd-init container logs from the etcd-0 pod in the
// control plane namespace and verifies that they contain expected snapshot restore traces.
// The expected log lines indicate a successful snapshot download and restore:
//   - "snapshot downloaded successfully"
//   - "snapshot restore succeeded" or "etcd snapshot restored successfully"
func VerifyEtcdInitLogs(ctx context.Context, logger logr.Logger, kubeClient kubernetes.Interface, controlPlaneNamespace string) error {
	podLogOpts := &corev1.PodLogOptions{
		Container: "etcd-init",
	}

	req := kubeClient.CoreV1().Pods(controlPlaneNamespace).GetLogs("etcd-0", podLogOpts)
	logStream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("failed to stream etcd-init container logs from etcd-0: %w", err)
	}
	defer logStream.Close()

	const tailSize = 50

	var (
		foundDownload bool
		foundRestore  bool
		lineCount     int
		tailLines     []string
	)

	scanner := bufio.NewScanner(logStream)
	buf := make([]byte, 256*1024)
	scanner.Buffer(buf, 512*1024)
	for scanner.Scan() {
		line := scanner.Text()
		lineCount++
		if len(tailLines) == tailSize {
			tailLines = tailLines[1:]
		}
		tailLines = append(tailLines, line)
		lower := strings.ToLower(line)
		if strings.Contains(lower, "snapshot downloaded successfully") {
			foundDownload = true
		}
		if strings.Contains(lower, "snapshot restore succeeded") || strings.Contains(lower, "etcd snapshot restored successfully") {
			foundRestore = true
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading etcd-init logs: %w", err)
	}

	logger.Info("etcd-init container logs scanned", "lines", lineCount)

	if !foundDownload {
		for _, line := range tailLines {
			logger.V(1).Info("etcd-init tail", "log", line)
		}
		return fmt.Errorf("etcd-init logs do not contain 'snapshot downloaded successfully'; snapshot download may have failed")
	}
	if !foundRestore {
		for _, line := range tailLines {
			logger.V(1).Info("etcd-init tail", "log", line)
		}
		return fmt.Errorf("etcd-init logs do not contain 'snapshot restore succeeded' or 'etcd snapshot restored successfully'; restore may have failed")
	}

	return nil
}
