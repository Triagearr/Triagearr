# Verifying a release

Every Triagearr release is signed with [cosign](https://github.com/sigstore/cosign)
keyless (OIDC) and ships with a CycloneDX SBOM per archive plus SLSA build
provenance attestations. Verification is optional but recommended for anything
running with `act: true`.

Replace `vX.Y.Z` with the release you downloaded.

## 1. Verify the signed checksum

This covers every archive **and** SBOM by transitivity — verifying `checksums.txt`
then checking your file against it is enough.

```bash
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp "https://github.com/Triagearr/Triagearr/.+" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

## 2. Verify the container image

```bash
cosign verify ghcr.io/triagearr/triagearr:vX.Y.Z \
  --certificate-identity-regexp "https://github.com/Triagearr/Triagearr/.+" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com"
```

## 3. Verify SLSA build provenance (requires the `gh` CLI)

```bash
gh attestation verify --owner Triagearr -- triagearr_*.tar.gz
gh attestation verify --owner Triagearr -- oci://ghcr.io/triagearr/triagearr:vX.Y.Z
```

See [`SECURITY.md`](../SECURITY.md) for the project's security policy and how to
report a vulnerability.
