ARG ROMM_IMAGE=ghcr.io/rommapp/romm:5.4.1@sha256:8677fb7b81b1d3db20b849e989a1dadc2a11159e51eb34a2acdcb01102274edf
FROM ${ROMM_IMAGE}
COPY --chmod=755 bin/linux-amd64/rom-meta /usr/local/bin/rom-meta
COPY examples/romm-importer/importer.py /adapter/importer.py
USER 568:568
WORKDIR /backend
ENTRYPOINT ["/src/.venv/bin/python", "-B", "/adapter/importer.py"]
