import React, { useEffect, useRef, useState } from 'react'
import {
  api,
  API,
  useAlert,
  Page,
  ListHeader,
  Card,
  SectionHeader,
  StatTile,
  KeyVal,
  StatusDot,
  Toggle,
  TextField,
  ModalConfirm,
  Loading,
  Button,
  ButtonText,
  Text,
  HStack,
  VStack
} from '@spr-networks/plugin-ui'

class MasqueAPI extends API {
  constructor() {
    super(`/plugins/${api.pluginURI() || 'spr-masque'}/`)
  }
  status() {
    return this.get('status')
  }
  config() {
    return this.get('config')
  }
  saveConfig(c) {
    return this.put('config', c)
  }
  register(body) {
    return this.post('register', body)
  }
  restart() {
    return this.post('restart', {})
  }
}

const masque = new MasqueAPI()

export default function Plugin() {
  const alert = useAlert()
  const [status, setStatus] = useState(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)

  // settings form state
  const [socksPort, setSocksPort] = useState('1080')
  const [useV6, setUseV6] = useState(false)
  const [dnsServers, setDnsServers] = useState('')
  const [deviceName, setDeviceName] = useState('spr-masque')
  const [connectPort, setConnectPort] = useState('443')
  const [jwt, setJwt] = useState('')
  const [showReRegister, setShowReRegister] = useState(false)
  const configLoaded = useRef(false)

  const refreshStatus = () => {
    masque
      .status()
      .then(setStatus)
      .catch((err) => alert.error('Failed to load status', err))
      .finally(() => setLoading(false))
  }

  const loadConfig = () => {
    masque
      .config()
      .then((c) => {
        setSocksPort(String(c.SocksPort ?? 1080))
        setConnectPort(String(c.ConnectPort ?? 443))
        setUseV6(c.EndpointVersion === 'v6')
        setDnsServers((c.DNSServers || []).join(', '))
        setDeviceName(c.DeviceName || 'spr-masque')
        configLoaded.current = true
      })
      .catch((err) => alert.error('Failed to load config', err))
  }

  useEffect(() => {
    refreshStatus()
    loadConfig()
    const t = setInterval(refreshStatus, 15000)
    return () => clearInterval(t)
  }, [])

  const doRegister = (force) => {
    setBusy(true)
    masque
      .register({ DeviceName: deviceName, JWT: jwt, Force: !!force })
      .then(() => {
        alert.success('Registered with Cloudflare WARP')
        refreshStatus()
      })
      .catch((err) => alert.error('Registration failed', err))
      .finally(() => setBusy(false))
  }

  const saveSettings = () => {
    const dns = dnsServers
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length)
    const cfg = {
      EndpointVersion: useV6 ? 'v6' : 'v4',
      SocksPort: parseInt(socksPort, 10) || 0,
      ConnectPort: parseInt(connectPort, 10) || 0,
      DNSServers: dns,
      DeviceName: deviceName
    }
    masque
      .saveConfig(cfg)
      .then(() => {
        alert.success('Saved — proxy restarted')
        refreshStatus()
      })
      .catch((err) => alert.error('Failed to save', err))
  }

  const doRestart = () => {
    masque
      .restart()
      .then(() => {
        alert.success('Proxy restarted')
        refreshStatus()
      })
      .catch((err) => alert.error('Restart failed', err))
  }

  if (loading) {
    return (
      <Page>
        <Loading />
      </Page>
    )
  }

  const registered = !!status?.Registered
  const running = !!status?.ProxyRunning
  const conn = status?.Connectivity || {}
  const socksAddr = status?.BindAddress || ''

  return (
    <Page>
      <ListHeader
        title="MASQUE Proxy"
        description="Cloudflare WARP over MASQUE (HTTP/3), exposed as a SOCKS5 proxy via usque"
        mark="wm"
        status={running ? (conn.OK ? 'Connected' : 'Starting') : registered ? 'Registered' : 'Not enrolled'}
        statusAction={running && conn.OK ? 'success' : registered ? 'warning' : 'muted'}
      >
        <Button size="sm" variant="outline" onPress={refreshStatus}>
          <ButtonText>Refresh</ButtonText>
        </Button>
      </ListHeader>

      <Card>
        <SectionHeader
          title="Enrollment"
          right={<StatusDot online={registered} />}
        />
        {registered ? (
          <VStack space="md">
            <HStack flexWrap="wrap" gap="$2">
              <StatTile label="Device ID" value={status?.DeviceID || '—'} mono />
              <StatTile label="WARP IPv4" value={status?.WarpIPv4 || '—'} mono />
            </HStack>
            <HStack justifyContent="space-between" alignItems="center">
              <Text size="sm" color="$muted500">
                This device is enrolled with Cloudflare WARP.
              </Text>
              <Button
                size="xs"
                variant="outline"
                action="negative"
                isDisabled={busy}
                onPress={() => setShowReRegister(true)}
              >
                <ButtonText>Re-register</ButtonText>
              </Button>
            </HStack>
          </VStack>
        ) : (
          <VStack space="md">
            <Text size="sm" color="$muted500">
              Register a new WARP device to obtain tunnel credentials. By
              registering you accept the Cloudflare WARP Terms of Service.
            </Text>
            <TextField
              label="Device name"
              value={deviceName}
              onChangeText={setDeviceName}
              placeholder="spr-masque"
            />
            <TextField
              label="Zero Trust enrollment token (optional)"
              value={jwt}
              onChangeText={setJwt}
              placeholder="JWT from your team enrollment page"
              helper="Leave empty for a regular free WARP account"
              secureTextEntry
            />
            <Button size="sm" isDisabled={busy} onPress={() => doRegister(false)}>
              <ButtonText>{busy ? 'Registering…' : 'Register with Cloudflare'}</ButtonText>
            </Button>
          </VStack>
        )}
      </Card>

      <Card>
        <SectionHeader
          title="Status"
          right={<StatusDot online={running && conn.OK} warn={running && !conn.OK} />}
        />
        <HStack flexWrap="wrap" gap="$2">
          <StatTile label="Proxy" value={running ? 'Running' : 'Stopped'} />
          <StatTile
            label="WARP"
            value={conn.Warp ? conn.Warp : conn.Error ? 'error' : '—'}
          />
          <StatTile label="Colo" value={conn.Colo || '—'} mono />
          <StatTile label="Exit IP" value={conn.IP || '—'} mono />
          <StatTile label="Endpoint" value={status?.Endpoint || '—'} mono />
          <StatTile label="Uptime" value={status?.Uptime || '—'} mono />
        </HStack>
        {conn.Error ? (
          <Text size="xs" color="$muted500" mt="$2">
            Connectivity check failed: {conn.Error}
          </Text>
        ) : null}
        {status?.LastError ? (
          <Text size="xs" color="$muted500" mt="$2">
            Last proxy error: {status.LastError}
          </Text>
        ) : null}
        <HStack mt="$2">
          <Button
            size="xs"
            variant="outline"
            isDisabled={!registered}
            onPress={doRestart}
          >
            <ButtonText>Restart proxy</ButtonText>
          </Button>
        </HStack>
      </Card>

      <Card>
        <SectionHeader title="Settings" />
        <VStack space="md">
          <TextField
            label="SOCKS5 port"
            value={socksPort}
            onChangeText={setSocksPort}
            placeholder="1080"
            helper="Listener binds to the container IP on the spr-masque bridge"
          />
          <TextField
            label="MASQUE connect port"
            value={connectPort}
            onChangeText={setConnectPort}
            placeholder="443"
            helper="UDP port used to reach the Cloudflare endpoint"
          />
          <TextField
            label="Tunnel DNS servers"
            value={dnsServers}
            onChangeText={setDnsServers}
            placeholder="9.9.9.9, 149.112.112.112"
            helper="Comma separated. Empty uses usque defaults (Quad9), resolved inside the tunnel"
          />
          <HStack justifyContent="space-between" alignItems="center">
            <Text size="sm">Use IPv6 Cloudflare endpoint</Text>
            <Toggle value={useV6} onPress={() => setUseV6(!useV6)} />
          </HStack>
          <Button size="sm" onPress={saveSettings}>
            <ButtonText>Save</ButtonText>
          </Button>
        </VStack>
      </Card>

      <Card>
        <SectionHeader title="How to use" />
        <VStack space="md">
          <KeyVal label="SOCKS5 proxy" value={socksAddr || 'proxy not up yet'} mono />
          <Text size="sm" color="$muted500">
            1. Add a device to the "masque" group in SPR (Devices → edit →
            Groups) so it can reach this container.
          </Text>
          <Text size="sm" color="$muted500">
            2. Point the device's browser or app at SOCKS5 host{' '}
            {socksAddr ? socksAddr.split(':')[0] : '<container IP>'} port{' '}
            {socksAddr ? socksAddr.split(':')[1] : socksPort}. No
            authentication. TCP and UDP are tunneled through Cloudflare WARP.
          </Text>
          <Text size="sm" color="$muted500">
            3. Verify at cloudflare.com/cdn-cgi/trace — it should show
            warp=on.
          </Text>
        </VStack>
      </Card>

      <ModalConfirm
        isOpen={showReRegister}
        onClose={() => setShowReRegister(false)}
        onConfirm={() => {
          setShowReRegister(false)
          doRegister(true)
        }}
        title="Re-register device?"
        message="This discards the current WARP credentials (a backup is kept as config.json.bak) and enrolls a fresh device."
        confirmText="Re-register"
        destructive
      />
    </Page>
  )
}
