# proton-torpedo
Tailscaile with ProtonVPN exit node

This small docker package contains a small manager which allows you to easily switch between ProtonVPN exit nodes while using Tailscale.

## Usage

Copy the .env.example file to .env and fill in your credentials.

Then run:
```bash
docker-compose up -d
```

This should connect to tailscale and proton.

You can then access the web interface at http://<hostname>:8081 to switch between exit nodes.
The connection will be built based on the `SERVER_HOSTNAMES` env variable, this will be overwritten by manager, the rest of the variables will be simply copied from the .env file.

## Future plans

- Add OpenVPN support (gluetun supports this and part of the image already)
- Add more providers supported by gluetun

Or a different path:

- Switch to native wireguard instead of gluetun
  - For this we can load in the informations from https://github.com/tn3w/ProtonVPN-IPs repo
  - The frontend should get data from here and create the WG config on the fly
