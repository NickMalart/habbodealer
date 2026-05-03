export namespace main {
	
	export class RaffleParticipant {
	    username: string;
	    betCount: number;
	    tickets: number;
	    firstBet: string;
	    lastBet: string;
	
	    static createFrom(source: any = {}) {
	        return new RaffleParticipant(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.username = source["username"];
	        this.betCount = source["betCount"];
	        this.tickets = source["tickets"];
	        this.firstBet = source["firstBet"];
	        this.lastBet = source["lastBet"];
	    }
	}
	export class RaffleSession {
	    id: number;
	    startedAt: string;
	    scheduledEndAt?: string;
	    endedAt?: string;
	    bonusEvery: number;
	    participants: RaffleParticipant[];
	
	    static createFrom(source: any = {}) {
	        return new RaffleSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.startedAt = source["startedAt"];
	        this.scheduledEndAt = source["scheduledEndAt"];
	        this.endedAt = source["endedAt"];
	        this.bonusEvery = source["bonusEvery"];
	        this.participants = this.convertValues(source["participants"], RaffleParticipant);
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
	export class RaffleState {
	    connected: boolean;
	    inRoom: boolean;
	    enabled: boolean;
	    bonusEvery: number;
	    ticketAnnounceEnabled: boolean;
	    currentSession?: RaffleSession;
	    sessions: RaffleSession[];
	
	    static createFrom(source: any = {}) {
	        return new RaffleState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.inRoom = source["inRoom"];
	        this.enabled = source["enabled"];
	        this.bonusEvery = source["bonusEvery"];
	        this.ticketAnnounceEnabled = source["ticketAnnounceEnabled"];
	        this.currentSession = this.convertValues(source["currentSession"], RaffleSession);
	        this.sessions = this.convertValues(source["sessions"], RaffleSession);
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

