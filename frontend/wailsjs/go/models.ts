export namespace app {
	
	export class BatchResult {
	    profileId: string;
	    name: string;
	    ok: boolean;
	    err?: string;
	    status?: session.Status;
	
	    static createFrom(source: any = {}) {
	        return new BatchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profileId = source["profileId"];
	        this.name = source["name"];
	        this.ok = source["ok"];
	        this.err = source["err"];
	        this.status = this.convertValues(source["status"], session.Status);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BatchSummary {
	    total: number;
	    succeeded: number;
	    failed: number;
	    results: BatchResult[];
	
	    static createFrom(source: any = {}) {
	        return new BatchSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.succeeded = source["succeeded"];
	        this.failed = source["failed"];
	        this.results = this.convertValues(source["results"], BatchResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BundleImportFailure {
	    index: number;
	    name: string;
	    err: string;
	
	    static createFrom(source: any = {}) {
	        return new BundleImportFailure(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.name = source["name"];
	        this.err = source["err"];
	    }
	}
	export class BundleImportOptions {
	    newSeeds: boolean;
	    namePrefix?: string;
	    group?: string;
	
	    static createFrom(source: any = {}) {
	        return new BundleImportOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.newSeeds = source["newSeeds"];
	        this.namePrefix = source["namePrefix"];
	        this.group = source["group"];
	    }
	}
	export class BundleImportResult {
	    imported: number;
	    skippedNames?: string[];
	    failures?: BundleImportFailure[];
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new BundleImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.imported = source["imported"];
	        this.skippedNames = source["skippedNames"];
	        this.failures = this.convertValues(source["failures"], BundleImportFailure);
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateRequest {
	    name: string;
	    kind: string;
	    proxy?: model.Proxy;
	    geoOverride?: model.Geo;
	    kernelVersion?: string;
	    notes?: string;
	    disableSpoofing?: string[];
	    group?: string;
	    tags?: string[];
	    deviceLabel?: string;
	    startup?: model.Startup;
	    useSystemBrowser?: boolean;
	    matchHostGPU?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CreateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.proxy = this.convertValues(source["proxy"], model.Proxy);
	        this.geoOverride = this.convertValues(source["geoOverride"], model.Geo);
	        this.kernelVersion = source["kernelVersion"];
	        this.notes = source["notes"];
	        this.disableSpoofing = source["disableSpoofing"];
	        this.group = source["group"];
	        this.tags = source["tags"];
	        this.deviceLabel = source["deviceLabel"];
	        this.startup = this.convertValues(source["startup"], model.Startup);
	        this.useSystemBrowser = source["useSystemBrowser"];
	        this.matchHostGPU = source["matchHostGPU"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CredentialProtection {
	    encrypted: boolean;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new CredentialProtection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.encrypted = source["encrypted"];
	        this.detail = source["detail"];
	    }
	}
	export class GroupTree {
	    groups: store.GroupStat[];
	    unassigned: number;
	    total: number;
	    tags: store.TagStat[];
	
	    static createFrom(source: any = {}) {
	        return new GroupTree(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.groups = this.convertValues(source["groups"], store.GroupStat);
	        this.unassigned = source["unassigned"];
	        this.total = source["total"];
	        this.tags = this.convertValues(source["tags"], store.TagStat);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class HostGPUInfo {
	    family: string;
	    renderer: string;
	
	    static createFrom(source: any = {}) {
	        return new HostGPUInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.family = source["family"];
	        this.renderer = source["renderer"];
	    }
	}
	export class ImportRequest {
	    text: string;
	    namePrefix?: string;
	    group?: string;
	    tags?: string[];
	    kind?: string;
	    kernelVersion?: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.namePrefix = source["namePrefix"];
	        this.group = source["group"];
	        this.tags = source["tags"];
	        this.kind = source["kind"];
	        this.kernelVersion = source["kernelVersion"];
	    }
	}
	export class ProxyView {
	    scheme: string;
	    host: string;
	    port: number;
	    username?: string;
	    hasPassword: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProxyView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scheme = source["scheme"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.hasPassword = source["hasPassword"];
	    }
	}
	export class ProfileView {
	    id: string;
	    name: string;
	    kind: string;
	    seed: number;
	    profileDir: string;
	    proxy?: ProxyView;
	    geoOverride?: model.Geo;
	    kernelVersion?: string;
	    extraArgs?: string[];
	    notes?: string;
	    disableSpoofing?: string[];
	    group?: string;
	    tags?: string[];
	    deviceLabel?: string;
	    useSystemBrowser?: boolean;
	    startup?: model.Startup;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    lastUseAt: any;
	    state: string;
	    fingerprint?: model.Fingerprint;
	    geoSource?: string;
	    exit?: geo.ExitInfo;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ProfileView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.seed = source["seed"];
	        this.profileDir = source["profileDir"];
	        this.proxy = this.convertValues(source["proxy"], ProxyView);
	        this.geoOverride = this.convertValues(source["geoOverride"], model.Geo);
	        this.kernelVersion = source["kernelVersion"];
	        this.extraArgs = source["extraArgs"];
	        this.notes = source["notes"];
	        this.disableSpoofing = source["disableSpoofing"];
	        this.group = source["group"];
	        this.tags = source["tags"];
	        this.deviceLabel = source["deviceLabel"];
	        this.useSystemBrowser = source["useSystemBrowser"];
	        this.startup = this.convertValues(source["startup"], model.Startup);
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.lastUseAt = this.convertValues(source["lastUseAt"], null);
	        this.state = source["state"];
	        this.fingerprint = this.convertValues(source["fingerprint"], model.Fingerprint);
	        this.geoSource = source["geoSource"];
	        this.exit = this.convertValues(source["exit"], geo.ExitInfo);
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ImportResult {
	    created: ProfileView[];
	    parseFailed?: model.ProxyParseError[];
	    createFailed?: BatchResult[];
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.created = this.convertValues(source["created"], ProfileView);
	        this.parseFailed = this.convertValues(source["parseFailed"], model.ProxyParseError);
	        this.createFailed = this.convertValues(source["createFailed"], BatchResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ProxyCheck {
	    ok: boolean;
	    err?: string;
	    exit?: geo.ExitInfo;
	    aligned?: model.Geo;
	    warnings?: string[];
	    elapsedMs: number;
	
	    static createFrom(source: any = {}) {
	        return new ProxyCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.err = source["err"];
	        this.exit = this.convertValues(source["exit"], geo.ExitInfo);
	        this.aligned = this.convertValues(source["aligned"], model.Geo);
	        this.warnings = source["warnings"];
	        this.elapsedMs = source["elapsedMs"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class URLHandlerView {
	    registered: boolean;
	    isDefault: boolean;
	    supported: boolean;
	    profileId?: string;
	    profileName?: string;
	    incognito: boolean;
	
	    static createFrom(source: any = {}) {
	        return new URLHandlerView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.registered = source["registered"];
	        this.isDefault = source["isDefault"];
	        this.supported = source["supported"];
	        this.profileId = source["profileId"];
	        this.profileName = source["profileName"];
	        this.incognito = source["incognito"];
	    }
	}
	export class UpdateRequest {
	    id: string;
	    name: string;
	    proxy?: model.Proxy;
	    geoOverride?: model.Geo;
	    clearProxy: boolean;
	    kernelVersion?: string;
	    extraArgs?: string[];
	    notes?: string;
	    disableSpoofing?: string[];
	    confirmKernelChange: boolean;
	    group?: string;
	    tags?: string[];
	    deviceLabel?: string;
	    confirmDeviceChange: boolean;
	    startup?: model.Startup;
	    useSystemBrowser?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UpdateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.proxy = this.convertValues(source["proxy"], model.Proxy);
	        this.geoOverride = this.convertValues(source["geoOverride"], model.Geo);
	        this.clearProxy = source["clearProxy"];
	        this.kernelVersion = source["kernelVersion"];
	        this.extraArgs = source["extraArgs"];
	        this.notes = source["notes"];
	        this.disableSpoofing = source["disableSpoofing"];
	        this.confirmKernelChange = source["confirmKernelChange"];
	        this.group = source["group"];
	        this.tags = source["tags"];
	        this.deviceLabel = source["deviceLabel"];
	        this.confirmDeviceChange = source["confirmDeviceChange"];
	        this.startup = this.convertValues(source["startup"], model.Startup);
	        this.useSystemBrowser = source["useSystemBrowser"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace geo {
	
	export class ExitGeo {
	    countryCode: string;
	    region: string;
	
	    static createFrom(source: any = {}) {
	        return new ExitGeo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.countryCode = source["countryCode"];
	        this.region = source["region"];
	    }
	}
	export class ExitInfo {
	    ip: string;
	    asn?: number;
	    org?: string;
	    kind: string;
	    geo: ExitGeo;
	
	    static createFrom(source: any = {}) {
	        return new ExitInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ip = source["ip"];
	        this.asn = source["asn"];
	        this.org = source["org"];
	        this.kind = source["kind"];
	        this.geo = this.convertValues(source["geo"], ExitGeo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace kernel {
	
	export class Kernel {
	    version: string;
	    execPath: string;
	    source?: string;
	    name?: string;
	
	    static createFrom(source: any = {}) {
	        return new Kernel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.execPath = source["execPath"];
	        this.source = source["source"];
	        this.name = source["name"];
	    }
	}
	export class Release {
	    version: string;
	    downloadUrl: string;
	    size: number;
	    assetName: string;
	
	    static createFrom(source: any = {}) {
	        return new Release(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.downloadUrl = source["downloadUrl"];
	        this.size = source["size"];
	        this.assetName = source["assetName"];
	    }
	}

}

export namespace model {
	
	export class DeviceProfile {
	    label: string;
	    platform: string;
	    platformVersion: string;
	    gpuVendor: string;
	    gpuRenderer: string;
	    hardwareConcurrency: number;
	    deviceMemory: number;
	    screenWidth: number;
	    screenHeight: number;
	    devicePixelRatio: number;
	    knownIssue?: string;
	
	    static createFrom(source: any = {}) {
	        return new DeviceProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.platform = source["platform"];
	        this.platformVersion = source["platformVersion"];
	        this.gpuVendor = source["gpuVendor"];
	        this.gpuRenderer = source["gpuRenderer"];
	        this.hardwareConcurrency = source["hardwareConcurrency"];
	        this.deviceMemory = source["deviceMemory"];
	        this.screenWidth = source["screenWidth"];
	        this.screenHeight = source["screenHeight"];
	        this.devicePixelRatio = source["devicePixelRatio"];
	        this.knownIssue = source["knownIssue"];
	    }
	}
	export class Fingerprint {
	    seed: number;
	    device: DeviceProfile;
	    brand: string;
	    brandVersion: string;
	    timezone: string;
	    locale: string;
	    acceptLanguages: string;
	
	    static createFrom(source: any = {}) {
	        return new Fingerprint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seed = source["seed"];
	        this.device = this.convertValues(source["device"], DeviceProfile);
	        this.brand = source["brand"];
	        this.brandVersion = source["brandVersion"];
	        this.timezone = source["timezone"];
	        this.locale = source["locale"];
	        this.acceptLanguages = source["acceptLanguages"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Geo {
	    countryCode: string;
	    timezone: string;
	    locale: string;
	    latitude: number;
	    longitude: number;
	
	    static createFrom(source: any = {}) {
	        return new Geo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.countryCode = source["countryCode"];
	        this.timezone = source["timezone"];
	        this.locale = source["locale"];
	        this.latitude = source["latitude"];
	        this.longitude = source["longitude"];
	    }
	}
	export class Proxy {
	    scheme: string;
	    host: string;
	    port: number;
	    username?: string;
	    password?: string;
	
	    static createFrom(source: any = {}) {
	        return new Proxy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scheme = source["scheme"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.password = source["password"];
	    }
	}
	export class ProxyParseError {
	    line: number;
	    raw: string;
	    err: string;
	
	    static createFrom(source: any = {}) {
	        return new ProxyParseError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.line = source["line"];
	        this.raw = source["raw"];
	        this.err = source["err"];
	    }
	}
	export class Startup {
	    mode?: string;
	    urls?: string[];
	    newTabUrl?: string;
	
	    static createFrom(source: any = {}) {
	        return new Startup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.urls = source["urls"];
	        this.newTabUrl = source["newTabUrl"];
	    }
	}

}

export namespace session {
	
	export class Status {
	    profileId: string;
	    state: string;
	    pid?: number;
	    // Go type: time
	    startedAt: any;
	    geo?: model.Geo;
	    exit?: geo.ExitInfo;
	    warnings?: string[];
	    fingerprint?: model.Fingerprint;
	    err?: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profileId = source["profileId"];
	        this.state = source["state"];
	        this.pid = source["pid"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.geo = this.convertValues(source["geo"], model.Geo);
	        this.exit = this.convertValues(source["exit"], geo.ExitInfo);
	        this.warnings = source["warnings"];
	        this.fingerprint = this.convertValues(source["fingerprint"], model.Fingerprint);
	        this.err = source["err"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace store {
	
	export class Filter {
	    group?: string;
	    tags?: string[];
	    keyword?: string;
	    kind?: string;
	
	    static createFrom(source: any = {}) {
	        return new Filter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.group = source["group"];
	        this.tags = source["tags"];
	        this.keyword = source["keyword"];
	        this.kind = source["kind"];
	    }
	}
	export class GroupStat {
	    name: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new GroupStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.count = source["count"];
	    }
	}
	export class TagStat {
	    name: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new TagStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.count = source["count"];
	    }
	}

}

