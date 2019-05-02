#!/usr/bin/env bash

namespace=gitlab
release=gitlab-demo
env=production

pushd $(mktemp -d)

# Args pattern, length
function gen_random(){
  head -c 4096 /dev/urandom | LC_CTYPE=C tr -cd $1 | head -c $2
}

# Args: secretname, args
function generate_secret_if_needed(){
  secret_args=( "${@:2}")
  secret_name=$1
  if ! $(kubectl --namespace=$namespace get secret $secret_name > /dev/null 2>&1); then
    kubectl --namespace=$namespace create secret generic $secret_name ${secret_args[@]}
  else
    echo "secret \"$secret_name\" already exists"
  fi;
  # Remove application labels if they exist
  kubectl --namespace=$namespace label \
    secret $secret_name $(echo 'app.kubernetes.io/name=gitlab-demo' | sed -E 's/=[^ ]*/-/g')
  kubectl --namespace=$namespace label \
    --overwrite \
    secret $secret_name app=shared-secrets chart=shared-secrets-0.1.0 release=gitlab-demo heritage=Tiller
}

# Initial root password
generate_secret_if_needed "gitlab-demo-gitlab-initial-root-password" --from-literal="password"=$(gen_random 'a-zA-Z0-9' 64)

# Redis password

# Gitlab shell
generate_secret_if_needed "gitlab-demo-gitlab-shell-secret" --from-literal="secret"=$(gen_random 'a-zA-Z0-9' 64)

# Gitaly secret
generate_secret_if_needed "gitlab-demo-gitaly-secret" --from-literal="token"=$(gen_random 'a-zA-Z0-9' 64)# Gitlab runner secret
generate_secret_if_needed "gitlab-demo-gitlab-runner-secret" --from-literal=runner-registration-token=$(gen_random 'a-zA-Z0-9' 64) --from-literal=runner-token=""

# Registry certificates
mkdir -p certs
openssl req -new -newkey rsa:4096 -subj "/CN=gitlab-issuer" -nodes -x509 -keyout certs/registry-example-com.key -out certs/registry-example-com.crt -days 3650
generate_secret_if_needed "gitlab-demo-registry-secret" --from-file=registry-auth.key=certs/registry-example-com.key --from-file=registry-auth.crt=certs/registry-example-com.crt

# config/secrets.yaml
if [ -n "$env" ]; then
  secret_key_base=$(gen_random 'a-f0-9' 128) # equavilent to secureRandom.hex(64)
  otp_key_base=$(gen_random 'a-f0-9' 128) # equavilent to secureRandom.hex(64)
  db_key_base=$(gen_random 'a-f0-9' 128) # equavilent to secureRandom.hex(64)
  openid_connect_signing_key=$(openssl genrsa 2048);

  cat << EOF > secrets.yml
$env:
  secret_key_base: $secret_key_base
  otp_key_base: $otp_key_base
  db_key_base: $db_key_base
  openid_connect_signing_key: |
$(openssl genrsa 2048 | awk '{print "    " $0}')
EOF
  generate_secret_if_needed "gitlab-demo-rails-secret" --from-file secrets.yml
fi

# Shell ssh host keys
ssh-keygen -A
mkdir -p host_keys
cp /etc/ssh/ssh_host_* host_keys/
generate_secret_if_needed "gitlab-demo-gitlab-shell-host-keys" --from-file host_keys

# Gitlab-workhorse secret
generate_secret_if_needed "gitlab-demo-gitlab-workhorse-secret" --from-literal="shared_secret"=$(gen_random 'a-zA-Z0-9' 32 | base64)

# Registry http.secret secret
generate_secret_if_needed "gitlab-demo-registry-httpsecret" --from-literal="secret"=$(gen_random 'a-z0-9' 128 | base64)
