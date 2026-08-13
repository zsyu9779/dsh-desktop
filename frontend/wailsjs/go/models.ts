export namespace main {
	
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

