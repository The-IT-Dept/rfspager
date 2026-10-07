#!/usr/bin/env python3
"""Fail if any 64-bit native library in an Android App Bundle is not 16 KB aligned.

Google Play refuses to release a bundle whose arm64-v8a or x86_64 libraries
have PT_LOAD segments aligned below 16 KB ("Artifact does not support 16KB
page size"). Uploading to a testing track still succeeds, so without this
check the problem only surfaces when promoting to production.

Usage: check-16kb-alignment.py path/to/app-release.aab
"""
import struct
import sys
import zipfile

PAGE = 16 * 1024
ABIS = ("arm64-v8a", "x86_64")


def min_load_align(data: bytes):
    if data[:4] != b"\x7fELF" or data[4] != 2:  # 64-bit ELF only
        return None
    phoff = struct.unpack_from("<Q", data, 0x20)[0]
    phentsize, phnum = struct.unpack_from("<HH", data, 0x36)
    aligns = []
    for i in range(phnum):
        off = phoff + i * phentsize
        if struct.unpack_from("<I", data, off)[0] == 1:  # PT_LOAD
            aligns.append(struct.unpack_from("<Q", data, off + 0x30)[0])
    return min(aligns) if aligns else None


def main(path: str) -> int:
    bad, checked = [], 0
    with zipfile.ZipFile(path) as z:
        for name in z.namelist():
            parts = name.split("/")
            if len(parts) == 4 and parts[1] == "lib" and parts[2] in ABIS and name.endswith(".so"):
                align = min_load_align(z.read(name))
                if align is None:
                    continue
                checked += 1
                if align < PAGE:
                    bad.append(f"{parts[2]}/{parts[3]} (align {align})")
    print(f"checked {checked} 64-bit native libraries for 16 KB alignment")
    for b in bad:
        print(f"::error::not 16 KB aligned: {b}")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
