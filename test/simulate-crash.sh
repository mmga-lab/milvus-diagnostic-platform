#!/bin/bash

# 模拟 Milvus 崩溃测试脚本

echo "Creating test namespace..."
kubectl create namespace milvus-test || true

echo "Creating a test pod that will generate coredump..."
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: crash-test
  namespace: milvus-test
  labels:
    app.kubernetes.io/name: milvus
    app.kubernetes.io/instance: test-crash
spec:
  containers:
  - name: crash
    image: alpine:latest
    command: ["/bin/sh", "-c"]
    args:
      - |
        echo "Enabling core dumps..."
        ulimit -c unlimited
        echo "/tmp/core.%e.%p" > /proc/sys/kernel/core_pattern || true
        echo "Generating segfault in 5 seconds..."
        sleep 5
        # Generate segmentation fault
        sh -c 'kill -SEGV $$'
    securityContext:
      privileged: true
  restartPolicy: OnFailure
  volumes:
  - name: coredump
    hostPath:
      path: /var/lib/systemd/coredump
      type: DirectoryOrCreate
EOF

echo "Waiting for crash..."
sleep 10

echo "Checking pod status..."
kubectl get pod crash-test -n milvus-test

echo "Checking coredump agent logs..."
kubectl logs -l app=milvus-coredump-agent --tail=20 | grep -E "(crash|coredump|discover)"