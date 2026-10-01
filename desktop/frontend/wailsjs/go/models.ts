export namespace core {
	
	export class EngineConfig {
	    peer: string;
	    password: string;
	    device_id: string;
	    vk_links: string[];
	    workers: number;
	    obfs: string;
	    captcha_mode: string;
	    dns: string;
	    mtu: number;
	    tun_name: string;
	    android_fd?: number;
	    lan_bridge_enabled: boolean;
	    lan_bridge_port: number;
	    turn_tcp: boolean;
	    enable_routing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new EngineConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.peer = source["peer"];
	        this.password = source["password"];
	        this.device_id = source["device_id"];
	        this.vk_links = source["vk_links"];
	        this.workers = source["workers"];
	        this.obfs = source["obfs"];
	        this.captcha_mode = source["captcha_mode"];
	        this.dns = source["dns"];
	        this.mtu = source["mtu"];
	        this.tun_name = source["tun_name"];
	        this.android_fd = source["android_fd"];
	        this.lan_bridge_enabled = source["lan_bridge_enabled"];
	        this.lan_bridge_port = source["lan_bridge_port"];
	        this.turn_tcp = source["turn_tcp"];
	        this.enable_routing = source["enable_routing"];
	    }
	}
	export class EngineMetrics {
	    state: string;
	    assigned_ip: string;
	    external_ip?: string;
	    upload_bytes: number;
	    download_bytes: number;
	    upload_rate_bps: number;
	    download_rate_bps: number;
	    active_workers: number;
	    total_workers: number;
	    latency_ms: number;
	    uptime_seconds: number;
	
	    static createFrom(source: any = {}) {
	        return new EngineMetrics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.assigned_ip = source["assigned_ip"];
	        this.external_ip = source["external_ip"];
	        this.upload_bytes = source["upload_bytes"];
	        this.download_bytes = source["download_bytes"];
	        this.upload_rate_bps = source["upload_rate_bps"];
	        this.download_rate_bps = source["download_rate_bps"];
	        this.active_workers = source["active_workers"];
	        this.total_workers = source["total_workers"];
	        this.latency_ms = source["latency_ms"];
	        this.uptime_seconds = source["uptime_seconds"];
	    }
	}

}

