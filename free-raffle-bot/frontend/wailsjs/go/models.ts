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
	    raffleName?: string;
	    prizeName?: string;
	    prizeQty?: number;
	    bonusEvery: number;
	    webhookMessageId?: string;
	    participants: RaffleParticipant[];
	    winnerName?: string;
	    winnerTickets?: number;
	    winnerOdds?: string;
	    winnerDrawnAt?: string;
	    winnerMethod?: string;
	    winnerSummary?: string;
	    winnerProofUrl?: string;
	    sponsorEnabled: boolean;
	    sponsorName?: string;
	    sponsorRoomName?: string;
	
	    static createFrom(source: any = {}) {
	        return new RaffleSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.startedAt = source["startedAt"];
	        this.scheduledEndAt = source["scheduledEndAt"];
	        this.endedAt = source["endedAt"];
	        this.raffleName = source["raffleName"];
	        this.prizeName = source["prizeName"];
	        this.prizeQty = source["prizeQty"];
	        this.bonusEvery = source["bonusEvery"];
	        this.webhookMessageId = source["webhookMessageId"];
	        this.participants = this.convertValues(source["participants"], RaffleParticipant);
	        this.winnerName = source["winnerName"];
	        this.winnerTickets = source["winnerTickets"];
	        this.winnerOdds = source["winnerOdds"];
	        this.winnerDrawnAt = source["winnerDrawnAt"];
	        this.winnerMethod = source["winnerMethod"];
	        this.winnerSummary = source["winnerSummary"];
	        this.winnerProofUrl = source["winnerProofUrl"];
	        this.sponsorEnabled = source["sponsorEnabled"];
	        this.sponsorName = source["sponsorName"];
	        this.sponsorRoomName = source["sponsorRoomName"];
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
	export class RaffleSessionSummary {
	    id: number;
	    startedAt: string;
	    scheduledEndAt?: string;
	    endedAt?: string;
	    raffleName?: string;
	    prizeName?: string;
	    prizeQty?: number;
	    winnerName?: string;
	    dbId: number;
	
	    static createFrom(source: any = {}) {
	        return new RaffleSessionSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.startedAt = source["startedAt"];
	        this.scheduledEndAt = source["scheduledEndAt"];
	        this.endedAt = source["endedAt"];
	        this.raffleName = source["raffleName"];
	        this.prizeName = source["prizeName"];
	        this.prizeQty = source["prizeQty"];
	        this.winnerName = source["winnerName"];
	        this.dbId = source["dbId"];
	    }
	}
	export class RaffleState {
	    connected: boolean;
	    inRoom: boolean;
	    enabled: boolean;
	    bonusEvery: number;
	    ticketAnnounceEnabled: boolean;
	    ticketProgressEnabled: boolean;
	    raffleName: string;
	    rafflePrizeName: string;
	    rafflePrizeQty: number;
	    raffleHeroImageName: string;
	    raffleAutoUpdate: boolean;
	    raffleMessageId: string;
	    hypeShoutEnabled: boolean;
	    hypeShoutPhrase: string;
	    hypeShoutMinutes: number;
	    currentSession?: RaffleSession;
	    sessions: RaffleSessionSummary[];
	    sponsorEnabled: boolean;
	    sponsorName: string;
	    sponsorRoomName: string;
	
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
	        this.ticketProgressEnabled = source["ticketProgressEnabled"];
	        this.raffleName = source["raffleName"];
	        this.rafflePrizeName = source["rafflePrizeName"];
	        this.rafflePrizeQty = source["rafflePrizeQty"];
	        this.raffleHeroImageName = source["raffleHeroImageName"];
	        this.raffleAutoUpdate = source["raffleAutoUpdate"];
	        this.raffleMessageId = source["raffleMessageId"];
	        this.hypeShoutEnabled = source["hypeShoutEnabled"];
	        this.hypeShoutPhrase = source["hypeShoutPhrase"];
	        this.hypeShoutMinutes = source["hypeShoutMinutes"];
	        this.currentSession = this.convertValues(source["currentSession"], RaffleSession);
	        this.sessions = this.convertValues(source["sessions"], RaffleSessionSummary);
	        this.sponsorEnabled = source["sponsorEnabled"];
	        this.sponsorName = source["sponsorName"];
	        this.sponsorRoomName = source["sponsorRoomName"];
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
	export class TallyItem {
	    name: string;
	    wonQty: number;
	    lostQty: number;
	    netQty: number;
	
	    static createFrom(source: any = {}) {
	        return new TallyItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.wonQty = source["wonQty"];
	        this.lostQty = source["lostQty"];
	        this.netQty = source["netQty"];
	    }
	}
	export class SessionTally {
	    sessionDbId: number;
	    startedAt: string;
	    endedAt: string;
	    games: number;
	    items: TallyItem[];
	
	    static createFrom(source: any = {}) {
	        return new SessionTally(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionDbId = source["sessionDbId"];
	        this.startedAt = source["startedAt"];
	        this.endedAt = source["endedAt"];
	        this.games = source["games"];
	        this.items = this.convertValues(source["items"], TallyItem);
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

