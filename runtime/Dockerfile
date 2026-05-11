FROM us-central1-docker.pkg.dev/k8s-staging-images/agent-sandbox/python-runtime-sandbox:latest-main

USER root
RUN apt-get update && apt-get install -y --no-install-recommends git nodejs npm \
    && rm -rf /var/lib/apt/lists/*
COPY requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir -r /tmp/requirements.txt && rm /tmp/requirements.txt
RUN mkdir -p /workspace && chown 1000:1000 /workspace
COPY agent_run.py /usr/local/bin/agent-run
RUN chmod +x /usr/local/bin/agent-run
USER 1000
