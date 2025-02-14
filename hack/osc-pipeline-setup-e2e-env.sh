#!/bin/bash


set -euo pipefail

setupCeph() {
    sudo microceph cluster bootstrap
    sudo microceph.ceph config set global osd_pool_default_size 1
    sudo microceph.ceph config set global mon_allow_pool_delete true
    sudo microceph.ceph osd crush rule rm replicated_rule
    sudo microceph.ceph osd crush rule create-replicated replicated default osd
    sudo microceph.ceph config set osd osd_crush_chooseleaf_type 0

    for flag in nosnaptrim noscrub nobackfill norebalance norecover noscrub nodeep-scrub; do
        sudo microceph.ceph osd set $flag
    done

    sudo microceph disk add loop,1G,1
    # restarted is need for load disk immediatelly, otherwise you have to wait ~30mins
    sudo snap restart microceph

    for _ in $(seq 60); do
    if sudo microceph.ceph pg stat | grep -wF unknown; then
        sleep 1
    else
        break
    fi
    done
    sudo rm -rf /etc/ceph
    sudo ln -s /var/snap/microceph/current/conf/ /etc/ceph
    sudo microceph enable rgw

    sudo ceph osd pool create devpool 1
    sudo rbd create --size 256 --pool devpool test-img

    sudo chmod 644 /etc/ceph/ceph.client.admin.keyring
}

setupPermissionForTTY() {
    sudo sed -i '$a\ devpts /dev/pts devpts rw,nosuid,noexec,relatime,gid=5,mode=0660 0 0' /etc/fstab
    sudo systemctl daemon-reload
    sudo mount -o remount /dev/pts
}

setupCeph
setupPermissionForTTY
sudo systemctl restart libvirtd
