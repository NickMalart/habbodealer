export namespace main {
	
	export class AddPayoutCheckResult {
	    allowed: boolean;
	    reason: string;
	    uniqueCount: number;
	    exists: boolean;
	    maxUnique: number;
	    maxQty: number;
	    minQty: number;
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
	        this.minQty = source["minQty"];
	        this.chunks = source["chunks"];
	        this.player = source["player"];
	        this.item = source["item"];
	    }
	}
	export class BanEntry {
	    key: string;
	    label: string;
	    expiresAt: string;
	    remainingSeconds: number;
	    message: string;
	    isActive: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BanEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.expiresAt = source["expiresAt"];
	        this.remainingSeconds = source["remainingSeconds"];
	        this.message = source["message"];
	        this.isActive = source["isActive"];
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
	    minQtyPerUnique: number;
	
	    static createFrom(source: any = {}) {
	        return new PayoutSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.maxUniqueItems = source["maxUniqueItems"];
	        this.maxQtyPerUnique = source["maxQtyPerUnique"];
	        this.minQtyPerUnique = source["minQtyPerUnique"];
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
	export class TradeState {
	    active: boolean;
	    partner: string;
	    partnerId: number;
	    elapsedSeconds: number;
	    remainingSeconds: number;
	    maxOpenSeconds: number;
	    banDurationSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new TradeState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.partner = source["partner"];
	        this.partnerId = source["partnerId"];
	        this.elapsedSeconds = source["elapsedSeconds"];
	        this.remainingSeconds = source["remainingSeconds"];
	        this.maxOpenSeconds = source["maxOpenSeconds"];
	        this.banDurationSeconds = source["banDurationSeconds"];
	    }
	}

}

