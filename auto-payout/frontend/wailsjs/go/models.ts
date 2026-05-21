export namespace main {
	
	export class Payout {
	    id: string;
	    name: string;
	    itemName: string;
	    quantity: number;
	    status: string;
	    createdAt: string;
	
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

