#!/bin/bash
# Command line install alternative to the SPR UI plugin installer.
echo "Please enter your SPR path (/home/spr/super/)"
read -r SUPERDIR

if [ -z "$SUPERDIR" ]; then
    SUPERDIR="/home/spr/super/"
fi

export SUPERDIR

echo "Please enter your SPR API token:"
read -r SPR_API_TOKEN

if [ -z "$SPR_API_TOKEN" ]; then
  echo "need api token, generate one on the auth keys page"
  exit 1
fi

mkdir -p "$SUPERDIR/configs/plugins/spr-masque"

# Token used by SPR to talk to the plugin (InstallTokenPath convention).
echo -n "$SPR_API_TOKEN" > "$SUPERDIR/configs/plugins/spr-masque/api-token"
chmod 600 "$SUPERDIR/configs/plugins/spr-masque/api-token"

docker compose build
docker compose up -d

CONTAINER_IP=$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "spr-masque")
API=127.0.0.1

# Grant the container wan+dns and expose it to devices in the "masque" group.
curl "http://${API}/firewall/custom_interface" \
-H "Authorization: Bearer ${SPR_API_TOKEN}" \
-X 'PUT' \
--data-raw "{\"SrcIP\":\"${CONTAINER_IP}\",\"Interface\":\"spr-masque\",\"Policies\":[\"wan\",\"dns\"],\"Groups\":[\"masque\"]}"

docker compose restart

echo ""
echo "Done. Open the spr-masque plugin UI (or POST /register) to enroll with"
echo "Cloudflare WARP, then point devices in the 'masque' group at"
echo "socks5://${CONTAINER_IP}:1080"
