export namespace main {
	
	export class GameStats {
	    game: string;
	    totalRounds: number;
	    playerWins: number;
	    dealerWins: number;
	    playerWinRate: number;
	    dealerWinRate: number;
	
	    static createFrom(source: any = {}) {
	        return new GameStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.game = source["game"];
	        this.totalRounds = source["totalRounds"];
	        this.playerWins = source["playerWins"];
	        this.dealerWins = source["dealerWins"];
	        this.playerWinRate = source["playerWinRate"];
	        this.dealerWinRate = source["dealerWinRate"];
	    }
	}
	export class CasinoStats {
	    overall: GameStats;
	    byGame: Record<string, GameStats>;
	
	    static createFrom(source: any = {}) {
	        return new CasinoStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.overall = this.convertValues(source["overall"], GameStats);
	        this.byGame = this.convertValues(source["byGame"], GameStats, true);
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
	
	export class ItemStats {
	    name: string;
	    in: number;
	    out: number;
	    net: number;
	    sources?: string;
	
	    static createFrom(source: any = {}) {
	        return new ItemStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.in = source["in"];
	        this.out = source["out"];
	        this.net = source["net"];
	        this.sources = source["sources"];
	    }
	}
	export class PlayerStats {
	    name: string;
	    totalRounds: number;
	    playerWins: number;
	    dealerWins: number;
	    playerWinRate: number;
	    dealerWinRate: number;
	    dealerEdge: number;
	
	    static createFrom(source: any = {}) {
	        return new PlayerStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.totalRounds = source["totalRounds"];
	        this.playerWins = source["playerWins"];
	        this.dealerWins = source["dealerWins"];
	        this.playerWinRate = source["playerWinRate"];
	        this.dealerWinRate = source["dealerWinRate"];
	        this.dealerEdge = source["dealerEdge"];
	    }
	}

}

