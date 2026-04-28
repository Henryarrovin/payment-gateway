#!/bin/bash
set -e

echo "Creating namespace..."
kubectl apply -f namespace.yaml

echo "Creating secrets..."
# Generate .env.secrets if it doesn't exist
if [ ! -f /workspace/.env.secrets ]; then
    echo "▶ .env.secrets not found, generating..."
    cat > /workspace/.env.secrets << EOF
AUTH_DB_PASSWORD=postgres
AUTH_JWT_ACCESS_SECRET=$(openssl rand -hex 32)
AUTH_JWT_REFRESH_SECRET=$(openssl rand -hex 32)
AUTH_JWT_CANONICAL_SECRET=$(openssl rand -hex 32)
EOF
    echo "Secrets generated"
fi

# Verify secrets file has content
echo "Secrets file contents:"
cat /workspace/.env.secrets

# Create secret directly from file — no envsubst needed
kubectl create secret generic auth-secrets \
  --namespace auth \
  --from-env-file=/workspace/.env.secrets \
  --dry-run=client -o yaml | kubectl apply -f -

echo "Secret created"
kubectl get secret auth-secrets -n auth

echo "Creating configmap..."
kubectl apply -f configmap.yaml

echo "Deploying postgres..."
kubectl apply -f postgres/pvc.yaml
kubectl apply -f postgres/deployment.yaml
kubectl apply -f postgres/service.yaml

echo "Deploying redis..."
kubectl apply -f redis/pvc.yaml
kubectl apply -f redis/deployment.yaml
kubectl apply -f redis/service.yaml

echo "Deploying zookeeper..."
kubectl apply -f zookeeper/deployment.yaml
kubectl apply -f zookeeper/service.yaml

echo "Deploying kafka..."
kubectl apply -f kafka/deployment.yaml
kubectl apply -f kafka/service.yaml

echo "Creating logs PVC..."
kubectl apply -f logs/pvc.yaml

echo "Waiting for postgres..."
kubectl wait --namespace auth \
  --for=condition=ready pod \
  --selector=app=postgres \
  --timeout=90s

echo "Waiting for kafka..."
kubectl wait --namespace auth \
  --for=condition=ready pod \
  --selector=app=kafka \
  --timeout=90s

echo "Deploying auth-service..."
kubectl apply -f auth-service/deployment.yaml
kubectl apply -f auth-service/service.yaml

echo "Done!"
kubectl get all -n auth