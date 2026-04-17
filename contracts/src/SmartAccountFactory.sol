// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.28;

import {Create2} from "@openzeppelin/contracts/utils/Create2.sol";
import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";
import {IEntryPoint} from "@account-abstraction/contracts/interfaces/IEntryPoint.sol";
import {SmartAccount} from "./SmartAccount.sol";

/// @title SmartAccountFactory
/// @notice Deploys SmartAccount proxies via CREATE2 for deterministic, counterfactual addresses.
///         Follows the eth-infinitism SimpleAccountFactory pattern: each proxy is an ERC-1967
///         proxy pointing to a shared SmartAccount implementation deployed in the constructor.
///         The EntryPoint calls `createAccount` through the UserOperation's `initCode` field
///         on the first transaction from a new account.
contract SmartAccountFactory {
    /// @notice The shared SmartAccount implementation behind all proxies.
    SmartAccount public immutable accountImplementation;

    /// @notice Emitted when a new account proxy is deployed.
    event AccountCreated(address indexed account, address indexed owner);

    /// @notice Thrown when owner is the zero address.
    error InvalidOwner();

    /// @notice Deploys the SmartAccount implementation contract.
    /// @param entryPoint_ The EntryPoint v0.7 address passed to the implementation constructor.
    constructor(IEntryPoint entryPoint_) {
        accountImplementation = new SmartAccount(entryPoint_);
    }

    /// @notice Deploy a new SmartAccount proxy, or return the existing one if already deployed.
    ///         Called by the EntryPoint via `initCode` on the account's first UserOperation,
    ///         or directly by anyone who wants to pre-deploy an account.
    /// @param owner The EOA that will own the smart account.
    /// @param salt  A user-chosen salt for CREATE2 address derivation.
    /// @return ret The SmartAccount proxy (new or existing).
    function createAccount(address owner, uint256 salt) external returns (SmartAccount ret) {
        if (owner == address(0)) revert InvalidOwner();
        address addr = getAddress(owner, salt);
        if (addr.code.length > 0) {
            return SmartAccount(payable(addr));
        }
        ret = SmartAccount(
            payable(new ERC1967Proxy{salt: bytes32(salt)}(
                    address(accountImplementation), abi.encodeCall(SmartAccount.initialize, (owner))
                ))
        );
        emit AccountCreated(address(ret), owner);
    }

    /// @notice Compute the counterfactual address for an account that would be deployed
    ///         with the given owner and salt, without actually deploying it.
    /// @param owner The EOA that would own the smart account.
    /// @param salt  The CREATE2 salt.
    /// @return The deterministic address.
    function getAddress(address owner, uint256 salt) public view returns (address) {
        if (owner == address(0)) revert InvalidOwner();
        return Create2.computeAddress(
            bytes32(salt),
            keccak256(
                abi.encodePacked(
                    type(ERC1967Proxy).creationCode,
                    abi.encode(address(accountImplementation), abi.encodeCall(SmartAccount.initialize, (owner)))
                )
            )
        );
    }
}
