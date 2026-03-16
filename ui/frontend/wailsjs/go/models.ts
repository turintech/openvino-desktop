export namespace main {
	
	export class BenchmarkResult {
	    model_name: string;
	    task: string;
	    iterations: number;
	    min_latency_ms: number;
	    max_latency_ms: number;
	    avg_latency_ms: number;
	    p95_latency_ms: number;
	    throughput_rps: number;
	    latencies: number[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new BenchmarkResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model_name = source["model_name"];
	        this.task = source["task"];
	        this.iterations = source["iterations"];
	        this.min_latency_ms = source["min_latency_ms"];
	        this.max_latency_ms = source["max_latency_ms"];
	        this.avg_latency_ms = source["avg_latency_ms"];
	        this.p95_latency_ms = source["p95_latency_ms"];
	        this.throughput_rps = source["throughput_rps"];
	        this.latencies = source["latencies"];
	        this.error = source["error"];
	    }
	}
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
	    enabled_categories: string[];
	
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
	        this.enabled_categories = source["enabled_categories"];
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

