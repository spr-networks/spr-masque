import React, { useEffect, useState } from 'react'
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
  Box,
  Button,
  ButtonIcon,
  ButtonText,
  Heading,
  HStack,
  Icon,
  Pressable,
  Text,
  VStack,
  ChevronDownIcon,
  ChevronRightIcon,
  CopyIcon,
  GlobeIcon
} from '@spr-networks/plugin-ui'

class MasqueAPI extends API {
  constructor() {
    // SPR_API_URL already ends in "/". Keep this relative so API.fetch does
    // not produce //plugins/..., which the router redirects; plugin-ui treats
    // redirects as an auth failure and navigates the iframe to /auth/validate.
    super(`plugins/${api.pluginURI() || 'spr-masque'}/`)
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
  trace() {
    return this.get('trace')
  }
}

const masque = new MasqueAPI()

/* ---------- small building blocks ---------- */

const copyText = (value, alert) => {
  if (!value) return
  try {
    navigator.clipboard
      .writeText(value)
      .then(() => alert.success('Copied'))
      .catch(() => alert.error('Copy failed'))
  } catch (e) {
    alert.error('Copy failed')
  }
}

const CopyButton = ({ value }) => {
  const alert = useAlert()
  return (
    <Button
      size="xs"
      variant="outline"
      action="secondary"
      isDisabled={!value}
      onPress={() => copyText(value, alert)}
    >
      <ButtonIcon as={CopyIcon} mr="$1" />
      <ButtonText>Copy</ButtonText>
    </Button>
  )
}

const Step = ({ n, children }) => (
  <HStack space="md" alignItems="flex-start">
    <Box
      w={22}
      h={22}
      mt="$0.5"
      borderRadius="$full"
      bg="$primary600"
      alignItems="center"
      justifyContent="center"
      flexShrink={0}
      sx={{ _dark: { bg: '$primary500' } }}
    >
      <Text size="2xs" color="$white" fontWeight="$bold">
        {n}
      </Text>
    </Box>
    <Box flex={1}>{children}</Box>
  </HStack>
)

const Disclosure = ({ open, onToggle, label, children }) => (
  <VStack space="md">
    <Pressable onPress={onToggle}>
      <HStack space="xs" alignItems="center">
        <Icon
          as={open ? ChevronDownIcon : ChevronRightIcon}
          size="sm"
          color="$muted500"
        />
        <Text size="sm" color="$muted500" fontWeight="$medium">
          {label}
        </Text>
      </HStack>
    </Pressable>
    {open ? children : null}
  </VStack>
)

const MonoBlock = ({ children }) => (
  <Box
    borderRadius="$lg"
    borderWidth={1}
    borderColor="$muted200"
    bg="$backgroundContentLight"
    p="$3"
    sx={{
      _dark: { bg: '$backgroundContentDark', borderColor: '$borderColorCardDark' }
    }}
  >
    <Text
      size="xs"
      color="$textLight900"
      sx={{
        _dark: { color: '$textDark100' },
        '@base': {
          fontFamily: 'monospace',
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-all'
        }
      }}
    >
      {children}
    </Text>
  </Box>
)

/* ---------- validation ---------- */

const portError = (v) => {
  const s = String(v).trim()
  if (!/^\d+$/.test(s)) return 'Enter a port number'
  const n = parseInt(s, 10)
  if (n < 1 || n > 65535) return 'Port must be between 1 and 65535'
  return null
}

const deviceNameError = (v) => {
  const s = v.trim()
  if (!s) return null
  if (!/^[A-Za-z0-9._-]{1,64}$/.test(s))
    return 'Letters, digits, dot, dash and underscore only (max 64)'
  return null
}

const dnsError = (v) => {
  const parts = v
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length)
  for (const p of parts) {
    if (!/^[0-9A-Fa-f:.]+$/.test(p)) return `"${p}" is not an IP address`
  }
  return null
}

/* ---------- plugin ---------- */

export default function Plugin() {
  const alert = useAlert()

  const [status, setStatus] = useState(null)
  const [statusError, setStatusError] = useState(null)
  const [loading, setLoading] = useState(true)

  // enrollment
  const [deviceName, setDeviceName] = useState('spr-masque')
  const [jwt, setJwt] = useState('')
  const [showZeroTrust, setShowZeroTrust] = useState(false)
  const [registering, setRegistering] = useState(false)
  const [showReRegister, setShowReRegister] = useState(false)

  // settings form + saved snapshot (for dirty tracking)
  const [socksPort, setSocksPort] = useState('1080')
  const [connectPort, setConnectPort] = useState('443')
  const [useV6, setUseV6] = useState(false)
  const [dnsServers, setDnsServers] = useState('')
  const [savedCfg, setSavedCfg] = useState(null)
  const [saving, setSaving] = useState(false)

  // quiet actions
  const [restarting, setRestarting] = useState(false)
  const [tracing, setTracing] = useState(false)
  const [traceOut, setTraceOut] = useState(null)

  const refreshStatus = () =>
    masque
      .status()
      .then((s) => {
        setStatus(s)
        setStatusError(null)
      })
      .catch((err) => setStatusError(err))
      .finally(() => setLoading(false))

  const applyConfig = (c) => {
    setSocksPort(String(c.SocksPort ?? 1080))
    setConnectPort(String(c.ConnectPort ?? 443))
    setUseV6(c.EndpointVersion === 'v6')
    setDnsServers((c.DNSServers || []).join(', '))
    setDeviceName(c.DeviceName || 'spr-masque')
    setSavedCfg(c)
  }

  const loadConfig = () =>
    masque
      .config()
      .then(applyConfig)
      .catch(() => {})

  useEffect(() => {
    refreshStatus()
    loadConfig()
    const t = setInterval(refreshStatus, 15000)
    return () => clearInterval(t)
  }, [])

  /* ----- derived state ----- */

  const registered = !!status?.Registered
  const running = !!status?.ProxyRunning
  const conn = status?.Connectivity || {}
  const connOK = !!conn.OK
  const socksAddr = status?.BindAddress || ''
  const socksHost = socksAddr.includes(':')
    ? socksAddr.slice(0, socksAddr.lastIndexOf(':'))
    : socksAddr
  const socksPortLive = socksAddr.includes(':')
    ? socksAddr.slice(socksAddr.lastIndexOf(':') + 1)
    : socksPort

  const dnsList = dnsServers
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length)

  const errSocksPort = portError(socksPort)
  const errConnectPort = portError(connectPort)
  const errDns = dnsError(dnsServers)
  const errDeviceName = deviceNameError(deviceName)
  const settingsInvalid = !!(errSocksPort || errConnectPort || errDns)

  const dirty =
    !!savedCfg &&
    (String(savedCfg.SocksPort) !== socksPort.trim() ||
      String(savedCfg.ConnectPort) !== connectPort.trim() ||
      (savedCfg.EndpointVersion === 'v6') !== useV6 ||
      (savedCfg.DNSServers || []).join(', ') !== dnsList.join(', '))

  /* ----- actions ----- */

  const doRegister = (force) => {
    setRegistering(true)
    masque
      .register({ DeviceName: deviceName.trim(), JWT: jwt.trim(), Force: !!force })
      .then(() => {
        alert.success('Registered with Cloudflare WARP')
        setJwt('')
        refreshStatus()
        loadConfig()
      })
      .catch((err) => alert.error('Registration failed', err))
      .finally(() => setRegistering(false))
  }

  const saveSettings = () => {
    setSaving(true)
    const cfg = {
      EndpointVersion: useV6 ? 'v6' : 'v4',
      SocksPort: parseInt(socksPort, 10),
      ConnectPort: parseInt(connectPort, 10),
      DNSServers: dnsList,
      DeviceName: savedCfg?.DeviceName || deviceName.trim() || 'spr-masque'
    }
    masque
      .saveConfig(cfg)
      .then((saved) => {
        applyConfig(saved && saved.SocksPort ? saved : cfg)
        alert.success('Settings saved — proxy restarted')
        refreshStatus()
      })
      .catch((err) => alert.error('Failed to save settings', err))
      .finally(() => setSaving(false))
  }

  const doRestart = () => {
    setRestarting(true)
    masque
      .restart()
      .then(() => {
        alert.success('Proxy restarted')
        refreshStatus()
      })
      .catch((err) => alert.error('Restart failed', err))
      .finally(() => setRestarting(false))
  }

  const runTrace = () => {
    setTracing(true)
    masque
      .trace()
      .then((t) => setTraceOut(typeof t === 'string' ? t : JSON.stringify(t)))
      .catch((err) => alert.error('Trace failed', err))
      .finally(() => setTracing(false))
  }

  /* ----- header state word ----- */

  let stateWord = 'Not enrolled'
  let stateAction = 'muted'
  if (registered) {
    if (running && connOK) {
      stateWord = 'Connected'
      stateAction = 'success'
    } else if (running) {
      stateWord = 'Connecting'
      stateAction = 'warning'
    } else {
      stateWord = 'Stopped'
      stateAction = 'error'
    }
  }

  const header = (
    <ListHeader
      title="MASQUE Proxy"
      description="Cloudflare WARP over MASQUE (HTTP/3), exposed as a SOCKS5 proxy via usque"
      mark="wm"
      status={loading || (statusError && !status) ? undefined : stateWord}
      statusAction={stateAction}
    >
      <Button size="sm" variant="outline" action="secondary" onPress={refreshStatus}>
        <ButtonText>Refresh</ButtonText>
      </Button>
    </ListHeader>
  )

  /* ----- loading / unreachable ----- */

  if (loading) {
    return (
      <Page>
        {header}
        <Loading text="Contacting spr-masque…" />
      </Page>
    )
  }

  if (statusError && !status) {
    return (
      <Page>
        {header}
        <Card>
          <VStack space="md" alignItems="center" py="$6">
            <Heading size="sm" color="$textLight900" sx={{ _dark: { color: '$textDark50' } }}>
              Can’t reach the plugin backend
            </Heading>
            <Text size="sm" color="$muted500" textAlign="center" maxWidth={420}>
              The spr-masque container may still be starting, or it’s stopped.
              Check Plugins → spr-masque if this persists.
            </Text>
            <Button size="sm" variant="outline" onPress={refreshStatus}>
              <ButtonText>Retry</ButtonText>
            </Button>
          </VStack>
        </Card>
      </Page>
    )
  }

  /* ----- first run: guided enrollment hero ----- */

  if (!registered) {
    return (
      <Page>
        {header}
        <Card>
          <VStack space="lg">
            <HStack space="md" alignItems="center">
              <Box
                w={44}
                h={44}
                borderRadius="$full"
                bg="$backgroundContentLight"
                alignItems="center"
                justifyContent="center"
                flexShrink={0}
                sx={{ _dark: { bg: '$backgroundContentDark' } }}
              >
                <Icon as={GlobeIcon} color="$primary600" size="xl" sx={{ _dark: { color: '$primary400' } }} />
              </Box>
              <VStack space="xs" flexShrink={1}>
                <Heading size="md" color="$textLight900" sx={{ _dark: { color: '$textDark50' } }}>
                  Connect this router to Cloudflare WARP
                </Heading>
                <Text size="sm" color="$muted500">
                  Enroll a WARP device once — selected SPR devices can then
                  browse through Cloudflare’s network via a local SOCKS5 proxy.
                </Text>
              </VStack>
            </HStack>

            <VStack space="md">
              <Step n="1">
                <Text size="sm" color="$muted500">
                  Register below. This creates a free WARP device (or a Zero
                  Trust one with a team token) and starts the tunnel.
                </Text>
              </Step>
              <Step n="2">
                <Text size="sm" color="$muted500">
                  Add devices to the “masque” group (Devices → edit → Groups)
                  so they can reach the proxy.
                </Text>
              </Step>
              <Step n="3">
                <Text size="sm" color="$muted500">
                  Point their browser or apps at the SOCKS5 address shown here
                  once connected.
                </Text>
              </Step>
            </VStack>

            <VStack space="md" maxWidth={480} w="$full">
              <TextField
                label="Device name"
                value={deviceName}
                onChangeText={setDeviceName}
                placeholder="spr-masque"
                helper="How this device appears in your Cloudflare dashboard"
                error={errDeviceName}
              />
              <Disclosure
                open={showZeroTrust}
                onToggle={() => setShowZeroTrust((v) => !v)}
                label="Zero Trust enrollment (optional)"
              >
                <TextField
                  label="Team enrollment token (JWT)"
                  value={jwt}
                  onChangeText={setJwt}
                  placeholder="Paste the JWT from your team enrollment page"
                  helper="Leave empty for a regular free WARP account"
                  secureTextEntry
                />
              </Disclosure>
              <Button
                size="md"
                alignSelf="flex-start"
                isDisabled={registering || !!errDeviceName}
                onPress={() => doRegister(false)}
              >
                <ButtonText>
                  {registering ? 'Registering…' : 'Register with Cloudflare'}
                </ButtonText>
              </Button>
              <Text size="2xs" color="$muted500">
                Registering accepts the Cloudflare WARP Terms of Service.
              </Text>
            </VStack>
          </VStack>
        </Card>
      </Page>
    )
  }

  /* ----- registered: overview first ----- */

  const heroTitle = running ? (connOK ? 'Connected' : 'Connecting') : 'Stopped'
  const heroSub =
    running && connOK
      ? `Cloudflare ${conn.Colo || '—'} · exit ${conn.IP || '—'}`
      : running
      ? conn.Error
        ? `Waiting for the tunnel — last check: ${conn.Error}`
        : 'Tunnel is coming up…'
      : status?.LastError
      ? `Proxy is not running — ${status.LastError}`
      : 'Proxy is not running'

  return (
    <Page>
      {header}

      {/* Overview hero: proxy state + exit identity */}
      <Card>
        <VStack space="lg">
          <HStack
            justifyContent="space-between"
            alignItems="center"
            flexWrap="wrap"
            gap="$3"
          >
            <HStack space="md" alignItems="center" flexShrink={1}>
              <StatusDot online={running && connOK} warn={running && !connOK} size={12} />
              <VStack space="xs">
                <Heading size="md" color="$textLight900" sx={{ _dark: { color: '$textDark50' } }}>
                  {heroTitle}
                </Heading>
                <Text
                  size="sm"
                  color="$muted500"
                  sx={
                    running && connOK
                      ? { '@base': { fontFamily: 'monospace' } }
                      : {}
                  }
                >
                  {heroSub}
                </Text>
              </VStack>
            </HStack>
            <HStack space="sm" alignItems="center">
              <Button
                size="xs"
                variant="outline"
                action="secondary"
                isDisabled={!running || tracing}
                onPress={runTrace}
              >
                <ButtonText>{tracing ? 'Tracing…' : 'Run trace'}</ButtonText>
              </Button>
              <Button
                size="xs"
                variant="outline"
                action="secondary"
                isDisabled={restarting}
                onPress={doRestart}
              >
                <ButtonText>{restarting ? 'Restarting…' : 'Restart proxy'}</ButtonText>
              </Button>
            </HStack>
          </HStack>

          <HStack flexWrap="wrap" gap="$2">
            <StatTile label="Endpoint" value={status?.Endpoint} mono />
            <StatTile
              label="Endpoint family"
              value={status?.EndpointVersion === 'v6' ? 'IPv6' : 'IPv4'}
            />
            <StatTile label="Uptime" value={running ? status?.Uptime : '—'} mono />
            <StatTile label="WARP IPv4" value={status?.WarpIPv4} mono />
          </HStack>

          {statusError ? (
            <Text size="xs" color="$muted500">
              Status refresh failed — retrying automatically.
            </Text>
          ) : null}

          {traceOut != null ? (
            <VStack space="sm">
              <HStack justifyContent="space-between" alignItems="center">
                <Text size="xs" color="$muted500">
                  cloudflare.com/cdn-cgi/trace — fetched through the proxy
                </Text>
                <HStack space="sm">
                  <CopyButton value={traceOut} />
                  <Button
                    size="xs"
                    variant="link"
                    action="secondary"
                    onPress={() => setTraceOut(null)}
                  >
                    <ButtonText>Hide</ButtonText>
                  </Button>
                </HStack>
              </HStack>
              <MonoBlock>{traceOut.trim()}</MonoBlock>
            </VStack>
          ) : null}
        </VStack>
      </Card>

      {/* How to use */}
      <Card>
        <SectionHeader title="How to use" />
        <VStack space="lg">
          <HStack
            justifyContent="space-between"
            alignItems="center"
            flexWrap="wrap"
            gap="$2"
          >
            <KeyVal label="SOCKS5 proxy" value={socksAddr || '—'} mono />
            <CopyButton value={socksAddr} />
          </HStack>

          <VStack space="md">
            <Step n="1">
              <Text size="sm" color="$muted500">
                Add a device to the “masque” group (Devices → edit → Groups) so
                it can reach this container. Nothing is exposed outside SPR.
              </Text>
            </Step>
            <Step n="2">
              <VStack space="sm">
                <Text size="sm" color="$muted500">
                  Configure its client for SOCKS5 (no authentication, TCP + UDP):
                </Text>
                <KeyVal
                  label="Firefox"
                  value={`Settings → Network Settings → Manual proxy → SOCKS Host ${socksHost || '<container IP>'}, Port ${socksPortLive}, SOCKS v5`}
                />
                <KeyVal
                  label="macOS"
                  value={`System Settings → Network → Details… → Proxies → SOCKS proxy ${socksHost || '<container IP>'} : ${socksPortLive}`}
                />
                <KeyVal
                  label="Android"
                  value="Use any app with SOCKS5 support (e.g. Firefox via about:config, or a proxy client)"
                />
              </VStack>
            </Step>
            <Step n="3">
              <VStack space="sm">
                <Text size="sm" color="$muted500">
                  Verify from the device — the response should report warp=on:
                </Text>
                <HStack
                  justifyContent="space-between"
                  alignItems="center"
                  flexWrap="wrap"
                  gap="$2"
                >
                  <Box flexShrink={1}>
                    <MonoBlock>
                      {`curl --socks5-hostname ${socksAddr || '<host:port>'} https://www.cloudflare.com/cdn-cgi/trace`}
                    </MonoBlock>
                  </Box>
                  <CopyButton
                    value={`curl --socks5-hostname ${socksAddr} https://www.cloudflare.com/cdn-cgi/trace`}
                  />
                </HStack>
              </VStack>
            </Step>
          </VStack>
        </VStack>
      </Card>

      {/* Enrollment facts */}
      <Card>
        <SectionHeader title="Enrollment" right={<StatusDot online size={10} />} />
        <VStack space="md">
          <HStack
            justifyContent="space-between"
            alignItems="center"
            flexWrap="wrap"
            gap="$2"
          >
            <KeyVal label="Device ID" value={status?.DeviceID} mono />
            <CopyButton value={status?.DeviceID} />
          </HStack>
          <KeyVal label="Device name" value={savedCfg?.DeviceName || deviceName} mono />
          <KeyVal label="WARP IPv4" value={status?.WarpIPv4} mono />
          <KeyVal label="WARP IPv6" value={status?.WarpIPv6} mono />
          <KeyVal
            label="Credentials"
            value={
              status?.HasPrivateKey && status?.HasAccessToken
                ? 'Configured ✓ (stored 0600, never shown)'
                : 'Incomplete — re-register'
            }
          />

          <Box
            borderTopWidth={1}
            borderColor="$borderColorCardLight"
            pt="$4"
            sx={{ _dark: { borderColor: '$borderColorCardDark' } }}
          >
            <VStack space="md">
              <Disclosure
                open={showZeroTrust}
                onToggle={() => setShowZeroTrust((v) => !v)}
                label="Zero Trust enrollment (optional)"
              >
                <TextField
                  label="Team enrollment token (JWT)"
                  value={jwt}
                  onChangeText={setJwt}
                  placeholder="Paste the JWT from your team enrollment page"
                  helper="Used only on the next re-registration. Leave empty for a free WARP account."
                  secureTextEntry
                />
              </Disclosure>
              <HStack
                justifyContent="space-between"
                alignItems="center"
                flexWrap="wrap"
                gap="$2"
              >
                <Text size="sm" color="$muted500" flexShrink={1}>
                  Re-registering enrolls a fresh device and invalidates this one.
                </Text>
                <Button
                  size="xs"
                  variant="outline"
                  action="negative"
                  isDisabled={registering}
                  onPress={() => setShowReRegister(true)}
                >
                  <ButtonText>{registering ? 'Registering…' : 'Re-register'}</ButtonText>
                </Button>
              </HStack>
            </VStack>
          </Box>
        </VStack>
      </Card>

      {/* Settings */}
      <Card>
        <SectionHeader title="Settings" />
        <VStack space="md" maxWidth={480} w="$full">
          <TextField
            label="SOCKS5 port"
            value={socksPort}
            onChangeText={setSocksPort}
            placeholder="1080"
            helper="Listener binds to the container IP on the spr-masque bridge"
            error={errSocksPort}
          />
          <TextField
            label="MASQUE connect port"
            value={connectPort}
            onChangeText={setConnectPort}
            placeholder="443"
            helper="UDP port used to reach the Cloudflare endpoint (keep 443 unless it’s blocked)"
            error={errConnectPort}
          />
          <TextField
            label="Tunnel DNS servers"
            value={dnsServers}
            onChangeText={setDnsServers}
            placeholder="9.9.9.9, 149.112.112.112"
            helper="Comma separated, resolved inside the tunnel. Empty uses usque defaults (Quad9)."
            error={errDns}
          />
          <HStack justifyContent="space-between" alignItems="center">
            <VStack space="xs" flexShrink={1}>
              <Text size="sm" color="$textLight900" sx={{ _dark: { color: '$textDark100' } }}>
                Use IPv6 Cloudflare endpoint
              </Text>
              <Text size="xs" color="$muted500">
                Connect to Cloudflare over IPv6 instead of IPv4
              </Text>
            </VStack>
            <Toggle
              value={useV6}
              onPress={() => setUseV6(!useV6)}
              label="Use IPv6 Cloudflare endpoint"
            />
          </HStack>
          <HStack space="md" alignItems="center" flexWrap="wrap" gap="$2">
            <Button
              size="sm"
              isDisabled={!dirty || saving || settingsInvalid}
              onPress={saveSettings}
            >
              <ButtonText>{saving ? 'Saving…' : 'Save changes'}</ButtonText>
            </Button>
            <Text size="xs" color="$muted500">
              {dirty
                ? 'Unsaved changes — applying restarts the proxy'
                : 'Applying changes restarts the proxy'}
            </Text>
          </HStack>
        </VStack>
      </Card>

      <ModalConfirm
        isOpen={showReRegister}
        onClose={() => setShowReRegister(false)}
        onConfirm={() => {
          setShowReRegister(false)
          doRegister(true)
        }}
        title="Re-register with Cloudflare?"
        message={`This invalidates the current WARP device (${status?.DeviceID || 'unknown ID'}) and enrolls a fresh one. Clients using the proxy disconnect until enrollment completes. The old credentials are kept as config.json.bak.`}
        confirmText="Re-register"
        destructive
      />
    </Page>
  )
}
