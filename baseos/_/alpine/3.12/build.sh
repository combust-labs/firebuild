#!/bin/bash

set -eu

base="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# establish the org, os and the version of the os to build
# from the path of this build script execution
version=$( basename "$base" )
os=$( basename "$(dirname "$base")" )
org=$( basename "$(dirname "$(dirname "$base")")" )

build_dir="/tmp/builds/${org}_${os}_${version}"
dockerfile="${base}/Dockerfile"
filesystem_target="/firecracker/rootfs/${org}/${os}/${version}/root.ext4"

key_file="${org}-${os}-${version}"
tag_random=$(cat /dev/urandom | tr -dc 'a-z0-9' | fold -w 16 | head -n1)
image_tag="build/${os}-${tag_random}:${version}"

pre_build_dir=$(pwd)

echo "Generating a keypair..."
set +e
ssh-keygen -t rsa -b 4096 -C "${os}-${version}@${org}" -f "${HOME}/.ssh/${key_file}"
set -e

echo "Creating build directory..."
mkdir -p "${build_dir}" && cd "${build_dir}"

echo "Copying public key to the build directory..."
cp "${HOME}/.ssh/${key_file}.pub" "${build_dir}/key.pub"

echo "Building Docker image..."
cp "${dockerfile}" "${build_dir}/Dockerfile"
docker build -t "${image_tag}" .
retVal=$?
cd "${pre_build_dir}"
rm -r "${build_dir}"

if [ $retVal -ne 0 ]; then
        echo " ==> build failed with status $?"
        exit $retVal
fi

echo "Creating file system..."
mkdir -p "${build_dir}/fsmnt"
dd if=/dev/zero of="${build_dir}/rootfs.ext4" bs=1M count=500
mkfs.ext4 "${build_dir}/rootfs.ext4"
echo "Mounting file system..."
sudo mount "${build_dir}/rootfs.ext4" "${build_dir}/fsmnt"

echo "Starting container from new image ${image_tag}..."
CONTAINER_ID=$(docker run --rm -v ${build_dir}/fsmnt:/export-rootfs -td ${image_tag} /bin/sh)

echo "Copying Docker file system..."
docker exec ${CONTAINER_ID} /bin/sh -c 'for d in home; do tar c "/$d" | tar x -C /export-rootfs; done; exit 0'
docker exec ${CONTAINER_ID} /bin/sh -c 'for d in bin dev etc lib root sbin usr; do tar c "/$d" | tar x -C /export-rootfs; done; exit 0'
docker exec ${CONTAINER_ID} /bin/sh -c 'for dir in proc run sys var; do mkdir /export-rootfs/${dir}; done; exit 0'

echo "Unmounting file system..."
sudo umount "${build_dir}/fsmnt"

echo "Removing docker container..."
docker stop $CONTAINER_ID

echo "Moving file system..."
mkdir -p "$( dirname "${filesystem_target}" )"
mv "${build_dir}/rootfs.ext4" "${filesystem_target}"

echo "Cleaning up build directory..."
rm -r "${build_dir}"

echo "Removing Docker image..."
docker rmi ${image_tag}

echo " \\o/ File system written to ${filesystem_target}."