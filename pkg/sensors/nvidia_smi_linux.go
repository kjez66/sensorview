//go:build linux

package sensors

// nvidiaSMICandidates returns the usual nvidia-smi locations on Linux.
func nvidiaSMICandidates() []string {
	return []string{
		"/usr/bin/nvidia-smi",
		"/usr/local/bin/nvidia-smi",
		"/opt/cuda/bin/nvidia-smi",
	}
}
