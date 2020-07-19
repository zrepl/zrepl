package tls

import "testing"

var fakeCertificateLoading bool

func FakeCertificateLoading(t *testing.T) {
	t.Logf("faking certificate loading")
	fakeCertificateLoading = true
}
