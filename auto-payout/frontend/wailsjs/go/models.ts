export namespace main {
	
	export class AddPayoutCheckResult {
	    allowed: boolean;
	    reason: string;
	    uniqueCount: number;
	    exists: boolean;
	    maxUnique: number;
	    maxQty: number;
	    chunks: number[];
	    player: string;
	    item: string;
	
	    static createFrom(source: any = {}) {
	        return new AddPayoutCheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.allowed = source["allowed"];
	        this.reason = source["reason"];
	        this.uniqueCount = source["uniqueCount"];
	        this.exists = source["exists"];
	        this.maxUnique = source["maxUnique"];
	        this.maxQty = source["maxQty"];
	        this.chunks = source["chunks"];
	        this.player = source["player"];
	        this.item = source["item"];
	    }
	}
	export class Payout {
	    id: string;
	    name: string;
	    itemName: string;
	    quantity: number;
	    status: string;
	    createdAt: string;
	    playerTradeId?: number;
	    bankerTradeId?: number;
	    notified?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Payout(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.itemName = source["itemName"];
	        this.quantity = source["quantity"];
	        this.status = source["status"];
	        this.createdAt = source["createdAt"];
	        this.playerTradeId = source["playerTradeId"];
	        this.bankerTradeId = source["bankerTradeId"];
	        this.notified = source["notified"];
	    }
	}
	export class PayoutSettings {
	    maxUniqueItems: number;
	    maxQtyPerUnique: number;
	
	    static createFrom(source: any = {}) {
	        return new PayoutSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.maxUniqueItems = source["maxUniqueItems"];
	        this.maxQtyPerUnique = source["maxQtyPerUnique"];
	    }
	}
	export class StockedItem {
	    id: number;
	    rawName: string;
	    canonicalName: string;
	    displayName: string;
	    isActive: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StockedItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.rawName = source["rawName"];
	        this.canonicalName = source["canonicalName"];
	        this.displayName = source["displayName"];
	        this.isActive = source["isActive"];
	    }
	}

}

