package haproxy

// ubuntuDriver is the systemd driver for Ubuntu (FRD §4).
type ubuntuDriver struct{ *linuxDriver }

// NewUbuntu returns the Ubuntu driver; every privileged operation goes through x.
func NewUbuntu(x Exec, opts DriverOptions) Driver {
	d := newLinuxDriver(OSUbuntu, x, opts, "/run/haproxy/admin.sock")
	d.baseline = d.linuxBaseline()
	return &ubuntuDriver{d}
}
