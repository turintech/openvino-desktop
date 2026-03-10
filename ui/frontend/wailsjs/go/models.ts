export namespace main {
	
	export class Config {
	    install_dir: string;
	    ovms_url: string;
	    uv_url: string;
	    startup_set: boolean;
	    search_tags: string[];
	    search_limit: number;
	    text_gen_target_device: string;
	    embeddings_target_device: string;
	    api_port: number;
	    ovms_rest_port: number;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.install_dir = source["install_dir"];
	        this.ovms_url = source["ovms_url"];
	        this.uv_url = source["uv_url"];
	        this.startup_set = source["startup_set"];
	        this.search_tags = source["search_tags"];
	        this.search_limit = source["search_limit"];
	        this.text_gen_target_device = source["text_gen_target_device"];
	        this.embeddings_target_device = source["embeddings_target_device"];
	        this.api_port = source["api_port"];
	        this.ovms_rest_port = source["ovms_rest_port"];
	    }
	}
	export class HFModel {
	    id: string;
	    pipeline_tag: string;
	    downloads: number;
	    likes: number;
	    library_name: string;
	
	    static createFrom(source: any = {}) {
	        return new HFModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.pipeline_tag = source["pipeline_tag"];
	        this.downloads = source["downloads"];
	        this.likes = source["likes"];
	        this.library_name = source["library_name"];
	    }
	}
	export class ModelInfo {
	    name: string;
	    base_path: string;
	    target_device: string;
	    task?: string;
	
	    static createFrom(source: any = {}) {
	        return new ModelInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.base_path = source["base_path"];
	        this.target_device = source["target_device"];
	        this.task = source["task"];
	    }
	}
	export class StatusResult {
	    deps_ready: boolean;
	    ovms_ready: boolean;
	    ovms_version: string;
	
	    static createFrom(source: any = {}) {
	        return new StatusResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deps_ready = source["deps_ready"];
	        this.ovms_ready = source["ovms_ready"];
	        this.ovms_version = source["ovms_version"];
	    }
	}

}

