# Frigate group_add uses numeric GIDs

- Date: 2026-11-15
- Related: [Frigate renderer](2026-11-14-frigate-renderer.md),
  [QEMU first boot](2026-11-14-qemu-firstboot.md),
  [defaults](../../defaults/README.md)

## What failed

The GK41 unit failed before the container started:

```text
Error response from daemon: Unable to find group render: no matching entries in group file
```

The host has `render:x:992:`. The rendered compose passed `group_add: [video, render]`. Docker
resolves those names against the container image's group file. The pinned upstream Frigate image has
a `video` entry but no `render` entry, so the name cannot resolve. The image is upstream and is not
Bittern's to modify. The device nodes carry the reference Debian GIDs, so no lookup is needed.

## What changed

`defaults/frigate.yaml` now gives the OpenVINO, VAAPI, and QSV policies the numeric GIDs 44 (video)
and 992 (render) instead of the names. The renderer rejects a non-numeric group entry while loading
defaults, so a future edit cannot quietly reintroduce a name. The compose goldens, the first-boot
camera golden, and the hand-written hardware fixture use the same numeric pair.

The rendered block for a GPU profile is:

```yaml
        group_add:
            - "44"
            - "992"
```

The QEMU firstboot guest detects CPU detection with no video decode, so its rendered compose has no
`group_add` block. The green firstboot run therefore does not exercise this path by itself. The
targeted check below used the live rendered compose as its base and added the numeric pair without
adding devices.

## QEMU evidence

The firstboot scenario passed on 2026-11-15 with KVM:

```text
generation 2, Frigate healthy, one camera, snapshot image/jpeg 10968 bytes
```

After the scenario, in the same appliance guest, a temporary compose copied from
`/var/lib/bittern/compose/compose.yaml` added only `group_add: ["44", "992"]`. The result:

```text
Health=healthy GroupAdd=["44","992"]
uid=0(root) gid=0(root) groups=0(root),44(video),992
Groups: 0 44 992
```

The same guest rejected the old form with the hardware error and accepted the numeric form:

```text
$ docker run --rm --network none --group-add render ... -c 'id'
docker: Error response from daemon: Unable to find group render: no matching entries in group file.
$ docker run --rm --network none --group-add 44 --group-add 992 ... -c 'id'
uid=0(root) gid=0(root) groups=0(root),44(video),992
```

The captured guest output is in `build/qemu/logs/firstboot-20261115-115718/group-gids.txt`
(gitignored). The unit was stopped for the temporary container and restarted healthy afterwards.
