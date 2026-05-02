export namespace main {
	
	export class TradeItem {
	    name: string;
	    quantity: number;
	    raw?: string;
	
	    static createFrom(source: any = {}) {
	        return new TradeItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.quantity = source["quantity"];
	        this.raw = source["raw"];
	    }
	}
	export class TradeEntry {
	    timestamp: string;
	    partnerName: string;
	    partnerTradeId: number;
	    payloadHex: string;
	    furniItems?: TradeItem[];
	
	    static createFrom(source: any = {}) {
	        return new TradeEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.partnerName = source["partnerName"];
	        this.partnerTradeId = source["partnerTradeId"];
	        this.payloadHex = source["payloadHex"];
	        this.furniItems = this.convertValues(source["furniItems"], TradeItem);
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
	export class TradeSession {
	    id: number;
	    startedAt: string;
	    endedAt?: string;
	    entries: TradeEntry[];
	
	    static createFrom(source: any = {}) {
	        return new TradeSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.startedAt = source["startedAt"];
	        this.endedAt = source["endedAt"];
	        this.entries = this.convertValues(source["entries"], TradeEntry);
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
	export class TrackerState {
	    connected: boolean;
	    running: boolean;
	    currentSession?: TradeSession;
	    sessions: TradeSession[];
	
	    static createFrom(source: any = {}) {
	        return new TrackerState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.running = source["running"];
	        this.currentSession = this.convertValues(source["currentSession"], TradeSession);
	        this.sessions = this.convertValues(source["sessions"], TradeSession);
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

