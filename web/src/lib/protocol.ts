// Wire types for the WebSocket protocol. These mirror internal/ws/messages.go;
// when that file changes, this one has to change with it.

export type SensorName = 'power' | 'lid' | 'usb' | 'screen' | 'network' | 'input';

export type AlertLevel = 'critical' | 'warning';

export type LocationSource = 'phone' | 'wifi' | 'ip';

export interface SensorInfo {
    name: string;
    display_name: string;
    available: boolean;
    enabled: boolean;
    /** Filled in from status updates rather than sent with the sensor list. */
    status?: string;
}

export interface SensorState {
    enabled: boolean;
    status: string;
}

export interface AlertData {
    sensor: string;
    level: AlertLevel;
    message: string;
}

export interface LocationFix {
    lat: number;
    lon: number;
    accuracy_m: number;
    source?: LocationSource;
    ts?: number;
    label?: string;
}

export interface LocationPayload {
    enabled: boolean;
    available: boolean;
    fix?: LocationFix;
    anchor?: LocationFix;
    moved_m?: number;
    moved?: boolean;
}

export interface LocationConfig {
    enabled: boolean;
    poll_seconds: number;
    phone_anchor: boolean;
    ip_fallback: boolean;
    wifi_enabled: boolean;
    geolocate_url?: string;
    has_geolocate_key: boolean;
    geolocate_key?: string;
}

export interface AlarmConfig {
    escalation_enabled: boolean;
}

export type UpdateChannel = 'stable' | 'beta';

/** What the laptop found when it last asked GitHub for a newer release. */
export interface UpdatePayload {
    running: string;
    latest: string;
    url: string;
    channel: string;
    /**
     * The upgrade command for however this copy was installed. Absent when the
     * laptop did not recognise the installation, and the URL is offered instead.
     */
    command?: string;
}

export interface AppConfig {
    port: number;
    max_sessions: number;
    max_auth_attempts: number;
    lockout_seconds: number;
    heartbeat_seconds: number;
    disconnect_grace_seconds: number;
    auto_arm_on_lock: boolean;
    input_threshold: number;
    connection_mode: string;
    update_check: boolean;
    update_channel?: string;
    update_check_hours?: number;
    remote_access: boolean;
    remote_port: number;
    alarm: AlarmConfig;
    pin_protection: { enabled: boolean; has_pin?: boolean; pin?: string };
    location: LocationConfig;
}

export interface ServerMessage {
    type: string;
    token?: string;
    version?: string;
    reason?: string;
    remaining_attempts?: number;
    sensors?: SensorInfo[];
    sensor_states?: Record<string, SensorState>;
    armed?: boolean;
    alert?: AlertData;
    ts?: number;
    config?: AppConfig;
    location?: LocationPayload;
    update?: UpdatePayload;
    /** SHA-256 fingerprint of the server's TLS certificate, sent with `hello`. */
    cert_fp?: string;
}

export interface ClientMessage {
    type: string;
    key?: string;
    token?: string;
    pin?: string;
    sensors?: Record<string, boolean>;
    sensor?: string;
    duration?: number;
    config?: Partial<AppConfig>;
    location?: LocationFix;
}

/** Descriptions shown when a sensor tile is expanded. */
export const SENSOR_DESCRIPTIONS: Record<string, string> = {
    power: 'Watches the charger. Fires when power is disconnected or reconnected.',
    lid: 'Watches the lid. Fires when it is opened or closed.',
    usb: 'Watches the USB ports. Fires when a device is plugged in or pulled out.',
    screen: 'Watches the display. Fires when the screen turns on or off.',
    network: 'Watches the network interfaces. Fires when the IP address changes.',
    input: 'Watches the keyboard and trackpad. Fires on any input while armed.',
};

/** Short captions for the annunciator tiles, in the panel's own voice. */
export const SENSOR_CAPTIONS: Record<string, string> = {
    power: 'POWER',
    lid: 'LID',
    usb: 'USB',
    screen: 'SCREEN',
    network: 'NETWORK',
    input: 'INPUT',
};
