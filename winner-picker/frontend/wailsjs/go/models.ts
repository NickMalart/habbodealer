export namespace main {
	
	export class AppState {
	    connected: boolean;
	    users: string[];
	
	    static createFrom(source: any = {}) {
	        return new AppState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.users = source["users"];
	    }
	}

}

