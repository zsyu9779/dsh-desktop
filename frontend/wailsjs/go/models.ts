export namespace main {
	
	export class deviceIdentity {
	    deviceId: string;
	    name: string;
	    publicKeyFp: string;
	    // Go type: time
	    issuedAt: any;
	    // Go type: time
	    lastActive: any;
	
	    static createFrom(source: any = {}) {
	        return new deviceIdentity(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deviceId = source["deviceId"];
	        this.name = source["name"];
	        this.publicKeyFp = source["publicKeyFp"];
	        this.issuedAt = this.convertValues(source["issuedAt"], null);
	        this.lastActive = this.convertValues(source["lastActive"], null);
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
	export class remoteStatus {
	    enabled: boolean;
	    allowPrivileged: boolean;
	    url: string;
	    pairingCode: string;
	    hostPublicKey: string;
	    certFingerprint: string;
	    qr: string;
	    port: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new remoteStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.allowPrivileged = source["allowPrivileged"];
	        this.url = source["url"];
	        this.pairingCode = source["pairingCode"];
	        this.hostPublicKey = source["hostPublicKey"];
	        this.certFingerprint = source["certFingerprint"];
	        this.qr = source["qr"];
	        this.port = source["port"];
	        this.message = source["message"];
	    }
	}
	export class status {
	    state: string;
	    url: string;
	    port: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.url = source["url"];
	        this.port = source["port"];
	        this.message = source["message"];
	    }
	}

}

