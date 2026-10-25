ARG ROMM_IMAGE=ghcr.io/rommapp/romm:5.4.0@sha256:03295f95a6290e752641f9553ac4198b0e5b71b879d57f7b2371b98f608feb54
FROM ${ROMM_IMAGE}
COPY --chmod=755 bin/linux-amd64/rom-meta /usr/local/bin/rom-meta
COPY examples/romm-importer/importer.py /adapter/importer.py
USER 568:568
WORKDIR /backend
ENTRYPOINT ["/src/.venv/bin/python", "-B", "/adapter/importer.py"]
