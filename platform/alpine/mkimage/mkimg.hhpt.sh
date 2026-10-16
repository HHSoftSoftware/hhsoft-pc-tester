profile_hhpt() {
	profile_standard
	profile_abbrev="hhpt"
	title="HHSoft PC Tester"
	desc="Offline hardware inventory and diagnostic environment."
	image_name="hhsoft-pc-tester"
	arch="aarch64 x86 x86_64"
	apks="alpine-base kbd-bkeymaps kmscube libdrm-tests mesa-dri-gallium mesa-egl mesa-gbm mesa-utils openssl pciutils usbutils util-linux"
	apkovl="genapkovl-hhpt.sh"
	hostname="hhpt"
	kernel_addons=
	initfs_features="$initfs_features kms"
	case "$ARCH" in
		aarch64) kernel_cmdline="console=tty0 console=ttyAMA0,115200" ;;
		*) kernel_cmdline="console=tty0 console=ttyS0,115200" ;;
	esac
	syslinux_prompt=0
	syslinux_timeout=10
	export HHPT_TARGET_ARCH="$ARCH"
}

section_apkovl() {
	[ -n "$apkovl" -a -n "$hostname" ] || return 0
	local inputs=$apkovl
	[ "$PROFILE" != hhpt ] || inputs="$inputs $HHPT_BINARY"
	build_section apkovl $hostname $(cat $inputs | checksum)
}
