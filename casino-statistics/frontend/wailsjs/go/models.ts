export namespace main {
	
	export class ItemStat {
	    name: string;
	    wonQty: number;
	    lostQty: number;
	    netQty: number;
	    games: number;
	
	    static createFrom(source: any = {}) {
	        return new ItemStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.wonQty = source["wonQty"];
	        this.lostQty = source["lostQty"];
	        this.netQty = source["netQty"];
	        this.games = source["games"];
	    }
	}
	export class GameStat {
	    game: string;
	    totalRounds: number;
	    playerWins: number;
	    dealerWins: number;
	    playerWinRate: number;
	    dealerWinRate: number;
	
	    static createFrom(source: any = {}) {
	        return new GameStat(source);
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
	    totalRounds: number;
	    playerWins: number;
	    dealerWins: number;
	    playerWinRate: number;
	    dealerWinRate: number;
	    byGame: Record<string, GameStat>;
	    items: ItemStat[];
	
	    static createFrom(source: any = {}) {
	        return new CasinoStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalRounds = source["totalRounds"];
	        this.playerWins = source["playerWins"];
	        this.dealerWins = source["dealerWins"];
	        this.playerWinRate = source["playerWinRate"];
	        this.dealerWinRate = source["dealerWinRate"];
	        this.byGame = this.convertValues(source["byGame"], GameStat, true);
	        this.items = this.convertValues(source["items"], ItemStat);
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

