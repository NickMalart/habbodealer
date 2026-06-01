export namespace main {
	
	export class RoomUser {
	    name: string;
	    chat_id: number;
	    trade_id: number;
	
	    static createFrom(source: any = {}) {
	        return new RoomUser(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.chat_id = source["chat_id"];
	        this.trade_id = source["trade_id"];
	    }
	}

}

