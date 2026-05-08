export namespace main {
	
	export class GEarthStatus {
	    exists: boolean;
	    running: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GEarthStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exists = source["exists"];
	        this.running = source["running"];
	    }
	}
	export class LaunchAppItem {
	    id: string;
	    name: string;
	    path: string;
	    exists: boolean;
	    running: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LaunchAppItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.exists = source["exists"];
	        this.running = source["running"];
	    }
	}

}

