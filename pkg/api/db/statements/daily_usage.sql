INSERT INTO daily_usage (cluster_id,resource_manager,num_units,project,groupname,username,last_updated_at,total_time_seconds,avg_cpu_usage,avg_cpu_mem_usage,total_cpu_energy_usage_kwh,total_cpu_emissions_gms,avg_gpu_usage,avg_gpu_mem_usage,total_gpu_energy_usage_kwh,total_gpu_emissions_gms,total_io_write_stats,total_io_read_stats,total_ingress_stats,total_outgress_stats,num_updates) VALUES (:cluster_id,:resource_manager,:num_units,:project,:groupname,:username,:last_updated_at,:total_time_seconds,:avg_cpu_usage,:avg_cpu_mem_usage,:total_cpu_energy_usage_kwh,:total_cpu_emissions_gms,:avg_gpu_usage,:avg_gpu_mem_usage,:total_gpu_energy_usage_kwh,:total_gpu_emissions_gms,:total_io_write_stats,:total_io_read_stats,:total_ingress_stats,:total_outgress_stats,:num_updates) ON CONFLICT(cluster_id,username,project,last_updated_at) DO UPDATE SET
  num_units = num_units + :num_units,
  total_time_seconds = add_metric_map(total_time_seconds, :total_time_seconds),
  avg_cpu_usage = avg_metric_map(avg_cpu_usage, :avg_cpu_usage, CAST(json_extract(total_time_seconds, '$.alloc_cputime') AS REAL), CAST(json_extract(:total_time_seconds, '$.alloc_cputime') AS REAL)),
  avg_cpu_mem_usage = avg_metric_map(avg_cpu_mem_usage, :avg_cpu_mem_usage, CAST(json_extract(total_time_seconds, '$.alloc_cpumemtime') AS REAL), CAST(json_extract(:total_time_seconds, '$.alloc_cpumemtime') AS REAL)),
  total_cpu_energy_usage_kwh = add_metric_map(total_cpu_energy_usage_kwh, :total_cpu_energy_usage_kwh),
  total_cpu_emissions_gms = add_metric_map(total_cpu_emissions_gms, :total_cpu_emissions_gms),
  avg_gpu_usage = avg_metric_map(avg_gpu_usage, :avg_gpu_usage, CAST(json_extract(total_time_seconds, '$.alloc_gputime') AS REAL), CAST(json_extract(:total_time_seconds, '$.alloc_gputime') AS REAL)),
  avg_gpu_mem_usage = avg_metric_map(avg_gpu_mem_usage, :avg_gpu_mem_usage, CAST(json_extract(total_time_seconds, '$.alloc_gpumemtime') AS REAL), CAST(json_extract(:total_time_seconds, '$.alloc_gpumemtime') AS REAL)),
  total_gpu_energy_usage_kwh = add_metric_map(total_gpu_energy_usage_kwh, :total_gpu_energy_usage_kwh),
  total_gpu_emissions_gms = add_metric_map(total_gpu_emissions_gms, :total_gpu_emissions_gms),
  total_io_write_stats = add_metric_map(total_io_write_stats, :total_io_write_stats),
  total_io_read_stats = add_metric_map(total_io_read_stats, :total_io_read_stats),
  total_ingress_stats = add_metric_map(total_ingress_stats, :total_ingress_stats),
  total_outgress_stats = add_metric_map(total_outgress_stats, :total_outgress_stats),
  num_updates = num_updates + :num_updates,
  last_updated_at = :last_updated_at
