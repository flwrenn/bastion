// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.28;

import {Script, console} from "forge-std/Script.sol";
import {IEntryPoint} from "@account-abstraction/contracts/interfaces/IEntryPoint.sol";
import {SmartAccountFactory} from "../src/SmartAccountFactory.sol";
import {Counter} from "../src/Counter.sol";
import {FaucetToken} from "../src/FaucetToken.sol";

/// @title Deploy
/// @notice Deploys the full Bastion contract suite to a target network.
///
///         Deployed contracts:
///         1. SmartAccountFactory — deploys a shared SmartAccount implementation
///            internally, then serves as the CREATE2 factory for account proxies.
///         2. Counter — demo target for SmartAccount interactions.
///         3. FaucetToken — ERC-20 faucet for testing token transfers.
///
///         Usage:
///           forge script script/Deploy.s.sol:Deploy \
///             --rpc-url sepolia \
///             --broadcast \
///             --verify \
///             --etherscan-api-key $ETHERSCAN_API_KEY \
///             -vvvv
contract Deploy is Script {
    /// @notice EntryPoint v0.7 — canonical address on all EVM chains.
    address constant ENTRYPOINT_V07 = 0x0000000071727De22E5E9d8BAf0edAc6f37da032;

    function run() external {
        vm.startBroadcast();

        SmartAccountFactory factory = new SmartAccountFactory(IEntryPoint(ENTRYPOINT_V07));
        Counter counter = new Counter();
        FaucetToken faucetToken = new FaucetToken();

        vm.stopBroadcast();

        console.log("--- Bastion Deployment ---");
        console.log("EntryPoint:           ", ENTRYPOINT_V07);
        console.log("SmartAccountFactory:  ", address(factory));
        console.log("  -> Implementation:  ", address(factory.accountImplementation()));
        console.log("Counter:              ", address(counter));
        console.log("FaucetToken:          ", address(faucetToken));

        // Write addresses to a JSON file for frontend consumption.
        string memory json = "deploy";
        vm.serializeAddress(json, "entryPoint", ENTRYPOINT_V07);
        vm.serializeAddress(json, "factory", address(factory));
        vm.serializeAddress(json, "accountImplementation", address(factory.accountImplementation()));
        vm.serializeAddress(json, "counter", address(counter));
        string memory output = vm.serializeAddress(json, "faucetToken", address(faucetToken));
        vm.writeJson(output, "./deployments/sepolia.json");
        console.log("Addresses written to deployments/sepolia.json");
    }
}
