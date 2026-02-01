package com.admin.config;

import lombok.extern.slf4j.Slf4j;
import org.springframework.boot.ApplicationArguments;
import org.springframework.boot.ApplicationRunner;
import org.springframework.scheduling.annotation.EnableScheduling;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

import javax.annotation.PreDestroy;
import javax.sql.DataSource;
import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.Statement;
import java.util.HashSet;
import java.util.Set;
import java.util.regex.Pattern;

/**
 * SQLite 数据库配置
 * 启用 WAL (Write-Ahead Logging) 模式以提高并发性能
 * 添加定期 checkpoint 和优雅关闭处理
 */
@Slf4j
@Component
@EnableScheduling
public class SQLiteConfig implements ApplicationRunner {

    private final DataSource dataSource;

    public SQLiteConfig(DataSource dataSource) {
        this.dataSource = dataSource;
    }

    @Override
    public void run(ApplicationArguments args) throws Exception {
        try (Connection connection = dataSource.getConnection();
             Statement statement = connection.createStatement()) {
            
            statement.execute("PRAGMA journal_mode=WAL;");
            statement.execute("PRAGMA synchronous=NORMAL;");
            statement.execute("PRAGMA cache_size=-64000;"); // 64MB 缓存
            statement.execute("PRAGMA temp_store=MEMORY;");
            statement.execute("PRAGMA busy_timeout=5000;"); // 5秒超时
            statement.execute("PRAGMA wal_autocheckpoint=1000;"); // 每1000页自动checkpoint

            ensureNodeDualStackColumns(connection);
             
            log.info("SQLite WAL mode configured successfully");
        } catch (Exception e) {
            log.error("Failed to configure SQLite database", e);
            throw e;
        }
    }

    private void ensureNodeDualStackColumns(Connection connection) throws Exception {
        Set<String> cols = getTableColumns(connection, "node");
        if (cols.isEmpty()) {
            return;
        }

        ensureColumnIfMissing(connection, cols, "node", "server_ip_v4", "VARCHAR(100)");
        ensureColumnIfMissing(connection, cols, "node", "server_ip_v6", "VARCHAR(100)");

        backfillNodeDualStackColumns(connection);
    }

    private void backfillNodeDualStackColumns(Connection connection) throws Exception {
        Pattern ipv4 = Pattern.compile("^(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$");

        try (Statement statement = connection.createStatement();
             ResultSet rs = statement.executeQuery("SELECT id, server_ip, server_ip_v4, server_ip_v6 FROM node;");
             PreparedStatement updV4 = connection.prepareStatement("UPDATE node SET server_ip_v4 = ? WHERE id = ?;");
             PreparedStatement updV6 = connection.prepareStatement("UPDATE node SET server_ip_v6 = ? WHERE id = ?;")
        ) {
            while (rs.next()) {
                long id = rs.getLong("id");
                String serverIp = rs.getString("server_ip");
                String v4 = rs.getString("server_ip_v4");
                String v6 = rs.getString("server_ip_v6");

                if (serverIp == null || serverIp.isBlank()) {
                    continue;
                }
                if ((v4 != null && !v4.isBlank()) || (v6 != null && !v6.isBlank())) {
                    continue;
                }

                String trimmed = serverIp.trim();

                if (ipv4.matcher(trimmed).matches()) {
                    updV4.setString(1, trimmed);
                    updV4.setLong(2, id);
                    updV4.executeUpdate();
                } else {
                    long colonCount = trimmed.chars().filter(ch -> ch == ':').count();
                    if (colonCount >= 2) {
                        updV6.setString(1, trimmed);
                        updV6.setLong(2, id);
                        updV6.executeUpdate();
                    }
                }
            }
        }
    }

    private Set<String> getTableColumns(Connection connection, String table) throws Exception {
        Set<String> cols = new HashSet<>();
        try (Statement statement = connection.createStatement();
             ResultSet rs = statement.executeQuery("PRAGMA table_info(" + table + ");")) {
            while (rs.next()) {
                String name = rs.getString("name");
                if (name != null && !name.isBlank()) {
                    cols.add(name);
                }
            }
        }
        return cols;
    }

    private void ensureColumnIfMissing(
            Connection connection,
            Set<String> existingColumns,
            String table,
            String column,
            String type
    ) throws Exception {
        if (existingColumns.contains(column)) {
            return;
        }

        try (Statement statement = connection.createStatement()) {
            statement.execute("ALTER TABLE " + table + " ADD COLUMN " + column + " " + type + ";");
        }

        log.info("SQLite schema updated: added {}.{}", table, column);
    }
    
    /**
     * 定期执行 checkpoint，确保 WAL 文件内容写入主数据库
     * 每5分钟执行一次
     */
    @Scheduled(fixedDelay = 300000, initialDelay = 300000)
    public void performCheckpoint() {
        try (Connection connection = dataSource.getConnection();
             Statement statement = connection.createStatement()) {
            
            statement.execute("PRAGMA wal_checkpoint(TRUNCATE);");
            log.debug("SQLite WAL checkpoint completed");
        } catch (Exception e) {
            log.error("Failed to perform SQLite checkpoint", e);
        }
    }
    
    /**
     * 应用关闭前执行最终的 checkpoint，确保所有数据都写入主数据库文件
     */
    @PreDestroy
    public void onShutdown() {
        log.info("Performing final SQLite checkpoint before shutdown...");
        try (Connection connection = dataSource.getConnection();
             Statement statement = connection.createStatement()) {
            
            // 强制执行 checkpoint，将所有 WAL 内容写入主数据库
            statement.execute("PRAGMA wal_checkpoint(TRUNCATE);");
            log.info("Final SQLite checkpoint completed successfully");
        } catch (Exception e) {
            log.error("Failed to perform final SQLite checkpoint", e);
        }
    }
}
