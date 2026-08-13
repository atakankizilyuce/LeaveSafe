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
    /**
     * Why this sensor is not watching, absent when it is.
     *
     * A sensor can be enabled and available and still not running — its driver
     * failed and the laptop is restarting the loop. Without this the panel
     * counted it as covered, which is the one thing an alarm must not get wrong.
     */
    failure?: string;
}

export interface SensorState {
    enabled: boolean;
    /** "ok", "unavailable" when the machine has no such sensor, or "failed". */
    status: string;
    failure?: string;
}

export interface AlertData {
    sensor: string;
    level: AlertLevel;
    message: string;
}

/**
 * The sensor name the laptop uses when an alert is about itself rather than
 * about something touching it — a setting that needs a restart, a change it
 * refused. No such sensor exists, which is the point: it marks the message as a
 * notice, not an intrusion.
 */
export const SYSTEM_NOTICE = 'system';

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

/** Somewhere the laptop can reach this phone while it is not connected. */
export interface PushSub {
    endpoint: string;
    key: string;
    auth: string;
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
    /** When arming happened, in Unix seconds. Sent with `auth_ok`. */
    armed_since?: number;
    alert?: AlertData;
    ts?: number;
    config?: AppConfig;
    location?: LocationPayload;
    update?: UpdatePayload;
    /**
     * The public half of the key the laptop signs push messages with, sent with
     * `auth_ok`. Without it this phone cannot subscribe to anything.
     *
     * It arrives with the successful pairing rather than with the greeting: it
     * is not a secret, but there is no reason to answer it to a connection that
     * has not proved it holds the pairing key.
     */
    push_key?: string;
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
    push?: PushSub;
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
