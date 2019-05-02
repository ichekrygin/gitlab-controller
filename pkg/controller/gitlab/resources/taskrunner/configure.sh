#!/usr/bin/env bash

set -e
config_dir="/init-config"
secret_dir="/init-secrets"

for secret in shell gitaly registry postgres rails-secrets ; do1
  mkdir -p "${secret_dir}/${secret}"
  cp -v -r -L "${config_dir}/${secret}/." "${secret_dir}/${secret}/"
done
for secret in redis minio objectstorage ldap omniauth smtp ; do
  if [ -e "${config_dir}/${secret}" ]; then
    mkdir -p "${secret_dir}/${secret}"
    cp -v -r -L "${config_dir}/${secret}/." "${secret_dir}/${secret}/"
  fi
done

if [ ! -f "/${secret_dir}/objectstorage/.s3cfg" ]; then
cat <<EOF > "/${secret_dir}/.s3cfg"
[default]
access_key = $(cat /init-secrets/minio/accesskey)
secret_key = $(cat /init-secrets/minio/secretkey)
bucket_location = us-east-1
host_base = minio-{{.HostSuffix}}.upbound.app
host_bucket = minio-{{.HostSuffix}}.upbound.app/%(bucket)
default_mime_type = binary/octet-stream
enable_multipart = True
multipart_max_chunks = 10000
recursive = True
recv_chunk = 65536
send_chunk = 65536
server_side_encryption = False
signature_v2 = True
socket_timeout = 300
use_mime_magic = True
verbosity = WARNING
website_endpoint = https://minio-{{.HostSuffix}}.upbound.app
EOF
else
  mv "/${secret_dir}/objectstorage/.s3cfg" "/${secret_dir}/.s3cfg"
fi

